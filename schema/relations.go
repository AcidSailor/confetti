package schema

import (
	"slices"

	"github.com/acidsailor/confetti/diag"
)

// Walk visits every definition reachable from the schema roots exactly once.
func (s *Schema) Walk(fn func(*Def)) {
	seen := map[*Def]bool{}
	var walk func([]*Def)
	walk = func(nodes []*Def) {
		for _, n := range nodes {
			if seen[n] {
				continue
			}
			seen[n] = true
			fn(n)
			walk(n.Children)
		}
	}
	walk(s.Roots)
}

// IsRef reports whether the relation matches a capture against a key elsewhere in the tree.
func (r Relation) IsRef() bool {
	return r.Scope == ScopeTree && r.Want == Present && r.FromArg != ""
}

// IsRequires reports whether the relation requires an instance with the label.
func (r Relation) IsRequires() bool {
	return r.Scope == ScopeTree && r.Want == Present && r.FromArg == ""
}

// IsExclusion reports whether the relation forbids a direct sibling with the label.
func (r Relation) IsExclusion() bool {
	return r.Scope == ScopeSiblings && r.Want == Absent
}

// spaceShape describes an exclusive name space.
type spaceShape struct {
	arity   int  // The exclusive arg count.
	listIdx int  // The List position among the exclusive args, or -1.
	anchor  *Def // The ScopedBy anchor, or nil.
	device  bool // Whether the space covers the whole configuration.
}

// shapeOf returns the exclusive name shape one definition declares.
func shapeOf(n *Def) spaceShape {
	args := n.ExclusiveArgs()
	s := spaceShape{arity: len(args), listIdx: -1}
	if n.ListSpec.Arg != "" {
		s.listIdx = slices.Index(args, n.ListSpec.Arg)
	}
	s.anchor, s.device = n.ScopeExtent()
	return s
}

// exclusiveSpace records declarations for one keyed name space.
type exclusiveSpace struct {
	members    int
	namespaced int  // Members declaring the label as their Namespace.
	declared   bool // Set when a member declares a Namespace or an extent.
	// shape comes from the first member; varied flags record conflicts.
	shape        spaceShape
	variedArity  bool
	variedList   bool
	variedExtent bool
}

// spaceIndex groups keyed definitions by exclusive label.
type spaceIndex struct {
	order  []string
	byName map[string]*exclusiveSpace
}

// add registers one keyed definition in the space its exclusive label names.
func (si *spaceIndex) add(n *Def) {
	// Invalid members must not define the shared shape.
	if len(n.KeyArgs) == 0 {
		return
	}
	if n.NamespaceLabel != "" && !n.HasLabel(n.NamespaceLabel) {
		return
	}
	label := n.ExclusiveLabel()
	// Unlabeled definitions do not share a space.
	if label == "" {
		return
	}
	if si.byName == nil {
		si.byName = map[string]*exclusiveSpace{}
	}
	got := shapeOf(n)
	sp := si.byName[label]
	if sp == nil {
		sp = &exclusiveSpace{shape: got}
		si.byName[label] = sp
		si.order = append(si.order, label)
	}
	sp.members++
	if n.NamespaceLabel == label {
		sp.namespaced++
	}
	sp.declared = sp.declared || n.DeclaresSpace()
	sp.variedArity = sp.variedArity || got.arity != sp.shape.arity
	sp.variedList = sp.variedList || got.listIdx != sp.shape.listIdx
	sp.variedExtent = sp.variedExtent ||
		got.anchor != sp.shape.anchor || got.device != sp.shape.device
}

// report reports conflicting shared-space shapes.
func (si spaceIndex) report(d *diag.Diagnostics) {
	// Report each label once to avoid declaration-order-dependent blame.
	for _, label := range si.order {
		sp := si.byName[label]
		if !sp.declared || sp.members < 2 {
			continue
		}
		for _, defect := range [...]struct {
			varied bool
			what   string
		}{
			{sp.variedArity, "exclusive arg count"},
			{sp.variedList, "which exclusive arg is a List"},
			{sp.variedExtent, "the exclusive-name extent"},
		} {
			if defect.varied {
				d.Add(diag.Error,
					"exclusive name space %q: members disagree on %s",
					label, defect.what)
			}
		}
	}
}

// ValidateRelations reports relation defects that depend on multiple definitions.
func (s *Schema) ValidateRelations(d *diag.Diagnostics) {
	rc := &relationCheck{
		kinds:  map[string]bool{},
		keysOf: map[string]map[string]bool{},
		d:      d,
	}
	// Every definition must be indexed before any definition is checked.
	s.Walk(rc.index)
	s.Walk(rc.check)
	s.checkScopeAnchors(d)
	rc.spaces.report(d)
}

// relationCheck contains cross-definition indexes.
type relationCheck struct {
	kinds  map[string]bool
	keysOf map[string]map[string]bool
	spaces spaceIndex
	d      *diag.Diagnostics
}

// index records a definition's labels, keys, and exclusive space.
func (rc *relationCheck) index(n *Def) {
	if n.KindName != "" {
		rc.kinds[n.KindName] = true
	}
	rc.spaces.add(n)
	for _, label := range n.Labels() {
		if rc.keysOf[label] == nil {
			rc.keysOf[label] = map[string]bool{}
		}
		for _, a := range n.KeyArgs {
			rc.keysOf[label][a] = true
		}
	}
}

// check validates a definition against the cross-definition indexes.
func (rc *relationCheck) check(n *Def) {
	// A label cannot be both identity-bearing and non-identity metadata.
	for _, name := range n.TagNames {
		if rc.kinds[name] {
			rc.d.Add(diag.Error,
				"%s: Tag %q collides with a Kind of the same name;"+
					" relations could resolve against either", n.Template, name)
		}
	}
	for _, rel := range n.Relations {
		n.checkRelation(rel, rc.keysOf, rc.d)
	}
	n.checkNamespace(rc.spaces, rc.d)
	n.checkScope(rc.d)
}

// checkScope reports exclusive declarations without a Key.
func (n *Def) checkScope(d *diag.Diagnostics) {
	if len(n.KeyArgs) > 0 {
		return
	}
	if n.ScopeAnchor != nil || n.DeviceScoped {
		d.Add(
			diag.Error,
			"%s: a declared exclusive scope needs a Key to hold a name",
			n.Template,
		)
	}
	if len(n.UniqueArgs) > 0 {
		d.Add(diag.Error, "%s: Unique needs a Key to hold a name", n.Template)
	}
}

// checkScopeAnchors requires each ScopedBy anchor on every path to its definition.
func (s *Schema) checkScopeAnchors(d *diag.Diagnostics) {
	var anchored []*Def
	s.Walk(func(n *Def) {
		if n.ScopeAnchor != nil {
			anchored = append(anchored, n)
		}
	})
	for _, n := range anchored {
		if s.reaches(n, n.ScopeAnchor) {
			d.Add(
				diag.Error,
				"%s: ScopedBy anchor %q is not an ancestor on every path to this definition",
				n.Template,
				n.ScopeAnchor.Template,
			)
		}
	}
}

// reaches reports whether any root reaches target without passing through skip.
func (s *Schema) reaches(target, skip *Def) bool {
	// Marking skip as seen removes every path through it; seen also ends recursion.
	seen := map[*Def]bool{skip: true}
	var walk func(*Def) bool
	walk = func(n *Def) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		return n == target || slices.ContainsFunc(n.Children, walk)
	}
	return slices.ContainsFunc(s.Roots, walk)
}

// checkNamespace validates a definition's namespace membership.
func (n *Def) checkNamespace(spaces spaceIndex, d *diag.Diagnostics) {
	if label := n.NamespaceLabel; label != "" {
		sp := spaces.byName[label]
		switch {
		case !n.HasLabel(label):
			d.Add(diag.Error,
				"%s: Namespace %q is not a Kind or Tag of this definition",
				n.Template, label)
		case len(n.KeyArgs) == 0:
			d.Add(
				diag.Error,
				"%s: Namespace %q needs a Key to hold a name",
				n.Template,
				label,
			)
		case sp == nil || sp.namespaced < 2:
			d.Add(diag.Error,
				"%s: Namespace %q has no other keyed member", n.Template, label)
		}
	}
	if len(n.KeyArgs) == 0 {
		return
	}
	// Every keyed carrier must participate in the shared namespace.
	for _, label := range n.Labels() {
		sp := spaces.byName[label]
		if sp != nil && sp.namespaced > 0 && n.NamespaceLabel != label {
			d.Add(
				diag.Error,
				"%s: carries label %q used as a Namespace but does not declare Namespace(%q)",
				n.Template,
				label,
				label,
			)
		}
	}
}

// checkRelation reports one relation whose label or key match cannot resolve against any definition.
func (n *Def) checkRelation(
	rel Relation,
	keysOf map[string]map[string]bool,
	d *diag.Diagnostics,
) {
	if rel.Scope != ScopeTree && rel.Scope != ScopeSiblings {
		d.Add(diag.Error, "%s: relation on label %q has invalid scope %d",
			n.Template, rel.Label, rel.Scope)
	}
	if rel.Want != Present && rel.Want != Absent {
		d.Add(diag.Error, "%s: relation on label %q has invalid polarity %d",
			n.Template, rel.Label, rel.Want)
	}
	if rel.Label == "" {
		d.Add(diag.Error, "%s: relation has an empty label", n.Template)
		return
	}
	if rel.Scope == ScopeTree && rel.Want == Absent ||
		rel.Scope == ScopeSiblings && rel.Want == Present {
		d.Add(diag.Error, "%s: relation on label %q is unsupported;"+
			" only tree-scope prerequisites and sibling exclusions are checked",
			n.Template, rel.Label)
	}
	switch {
	case rel.FromArg != "" && rel.TargetKey == "":
		d.Add(diag.Error,
			"%s: relation on label %q matches arg %q against no target key",
			n.Template, rel.Label, rel.FromArg)
	case rel.FromArg == "" && rel.TargetKey != "":
		d.Add(diag.Error,
			"%s: relation on label %q sets target key %q but matches no arg",
			n.Template, rel.Label, rel.TargetKey)
	case rel.FromArg != "" && n.spec.ArgType(rel.FromArg) == "":
		d.Add(diag.Error,
			"%s: relation on label %q matches %q, not a capture arg",
			n.Template, rel.Label, rel.FromArg)
	}
	keys, ok := keysOf[rel.Label]
	if !ok {
		d.Add(diag.Error, "%s: relation names undeclared label %q;"+
			" no definition declares it as a Kind or a Tag", n.Template, rel.Label)
		return
	}
	if rel.TargetKey != "" && !keys[rel.TargetKey] {
		d.Add(
			diag.Error,
			"%s: relation targets %q.%q but no definition carrying %q keys by %q",
			n.Template,
			rel.Label,
			rel.TargetKey,
			rel.Label,
			rel.TargetKey,
		)
	}
}
