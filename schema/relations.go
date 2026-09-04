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

// namespaceIndex records which labels scope exclusive resources and the exclusive arg count of the first declaring member.
type namespaceIndex struct {
	members map[string]int
	arity   map[string]int
}

// ValidateRelations reports relation defects that depend on multiple definitions.
func (s *Schema) ValidateRelations(d *diag.Diagnostics) {
	kinds := map[string]bool{}
	keysOf := map[string]map[string]bool{}
	ns := namespaceIndex{members: map[string]int{}, arity: map[string]int{}}
	s.Walk(func(n *Def) {
		if n.KindName != "" {
			kinds[n.KindName] = true
		}
		if label := n.NamespaceLabel; label != "" && len(n.KeyArgs) > 0 {
			if ns.members[label] == 0 {
				ns.arity[label] = len(n.ExclusiveArgs())
			}
			ns.members[label]++
		}
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
	})
}

// checkNamespace reports a Namespace this definition cannot join or one it silently stays out of.
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
		case ns.members[label] < 2:
			d.Add(diag.Error,
				"%s: Namespace %q has no other keyed member", n.Template, label)
		case ns.arity[label] != len(n.ExclusiveArgs()):
			d.Add(diag.Error,
				"%s: Namespace %q members disagree on exclusive arg count",
				n.Template, label)
		case n.ListSpec.Arg != "" &&
			slices.Contains(n.ExclusiveArgs(), n.ListSpec.Arg):
			// The commit check compares whole names, not list overlap.
			d.Add(diag.Error,
				"%s: Namespace %q cannot make List arg %q exclusive",
				n.Template, label, n.ListSpec.Arg)
		}
	}
	if len(n.KeyArgs) == 0 {
		return
	}
	// A keyed carrier outside the namespace would never release the shared name.
	for _, label := range n.Labels() {
		if ns.members[label] > 0 && n.NamespaceLabel != label {
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
