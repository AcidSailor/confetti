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

// nsMembers records what the keyed members of one Namespace label declare.
type nsMembers struct {
	count  int
	arity  int  // The exclusive arg count the first keyed member fixed.
	varied bool // Set when a later member declares a different count.
}

// namespaceIndex holds the keyed members of each Namespace label in declaration order.
type namespaceIndex struct {
	order  []string
	byName map[string]*nsMembers
}

// add registers one keyed member that carries the label it declares.
func (ns *namespaceIndex) add(n *Def) {
	label := n.NamespaceLabel
	// A member that fails the other checks must not fix the arity or the count.
	if label == "" || len(n.KeyArgs) == 0 || !n.HasLabel(label) {
		return
	}
	m := ns.byName[label]
	if m == nil {
		m = &nsMembers{arity: len(n.ExclusiveArgs())}
		ns.byName[label] = m
		ns.order = append(ns.order, label)
	}
	m.count++
	if len(n.ExclusiveArgs()) != m.arity {
		m.varied = true
	}
}

// ValidateRelations reports relation defects that depend on multiple definitions.
func (s *Schema) ValidateRelations(d *diag.Diagnostics) {
	kinds := map[string]bool{}
	keysOf := map[string]map[string]bool{}
	ns := namespaceIndex{byName: map[string]*nsMembers{}}
	s.Walk(func(n *Def) {
		if n.KindName != "" {
			kinds[n.KindName] = true
		}
		ns.add(n)
		for _, label := range n.Labels() {
			if keysOf[label] == nil {
				keysOf[label] = map[string]bool{}
			}
			for _, a := range n.KeyArgs {
				keysOf[label][a] = true
			}
		}
	})
	s.Walk(func(n *Def) {
		// A label cannot be both identity-bearing and non-identity metadata.
		for _, name := range n.TagNames {
			if kinds[name] {
				d.Add(diag.Error,
					"%s: Tag %q collides with a Kind of the same name;"+
						" relations could resolve against either", n.Template, name)
			}
		}
		for _, rel := range n.Relations {
			n.checkRelation(rel, keysOf, d)
		}
		n.checkNamespace(ns, d)
		n.checkScope(d)
	})
	s.checkScopeAnchors(d)
	// One report per label; blaming each member would depend on declaration order.
	for _, label := range ns.order {
		if ns.byName[label].varied {
			d.Add(diag.Error,
				"Namespace %q members disagree on exclusive arg count", label)
		}
	}
}

// checkScope reports a declared exclusive-name extent this definition cannot carry.
func (n *Def) checkScope(d *diag.Diagnostics) {
	if n.ScopeAnchor == nil && !n.DeviceScoped {
		return
	}
	if n.ScopeAnchor != nil && n.DeviceScoped {
		d.Add(
			diag.Error,
			"%s: ScopedBy and ScopedByDevice name different extents",
			n.Template,
		)
	}
	if len(n.KeyArgs) == 0 {
		d.Add(
			diag.Error,
			"%s: a declared exclusive scope needs a Key to hold a name",
			n.Template,
		)
	}
}

// checkScopeAnchors reports a ScopedBy anchor that is not an ancestor on every path to the definition naming it.
func (s *Schema) checkScopeAnchors(d *diag.Diagnostics) {
	var unanchored []*Def
	seen := map[*Def]bool{}
	var walk func(n *Def, chain []*Def)
	walk = func(n *Def, chain []*Def) {
		// A recursive grammar revisits the same definition without new ancestors.
		if slices.Contains(chain, n) {
			return
		}
		if a := n.ScopeAnchor; a != nil && !slices.Contains(chain, a) &&
			!seen[n] {
			seen[n] = true
			unanchored = append(unanchored, n)
		}
		chain = append(slices.Clone(chain), n)
		for _, c := range n.Children {
			walk(c, chain)
		}
	}
	for _, r := range s.Roots {
		walk(r, nil)
	}
	for _, n := range unanchored {
		d.Add(
			diag.Error,
			"%s: ScopedBy anchor %q is not an ancestor on every path to this definition",
			n.Template,
			n.ScopeAnchor.Template,
		)
	}
}

// checkNamespace reports the first defect that makes this definition's Namespace unusable, plus a keyed label it stays out of.
func (n *Def) checkNamespace(ns namespaceIndex, d *diag.Diagnostics) {
	if label := n.NamespaceLabel; label != "" {
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
		case ns.byName[label].count < 2:
			d.Add(diag.Error,
				"%s: Namespace %q has no other keyed member", n.Template, label)
		}
	}
	if len(n.KeyArgs) == 0 {
		return
	}
	// A keyed carrier outside the namespace would never release the shared name.
	for _, label := range n.Labels() {
		if ns.byName[label] != nil && n.NamespaceLabel != label {
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
	// Unsupported relation shapes are authoring errors.
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
