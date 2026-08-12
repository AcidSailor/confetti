package schema

import "github.com/acidsailor/confetti/diag"

// Walk visits every definition reachable from the schema roots exactly once.
func (s *Schema) Walk(fn func(*Node)) {
	seen := map[*Node]bool{}
	var walk func([]*Node)
	walk = func(nodes []*Node) {
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

// IsRef reports whether the relation matches a captured arg against a key elsewhere in the tree; see Ref.
func (r Relation) IsRef() bool {
	return r.Scope == ScopeTree && r.Want == Present && r.FromArg != ""
}

// IsRequires reports whether the relation demands any instance carrying the label; see Requires.
func (r Relation) IsRequires() bool {
	return r.Scope == ScopeTree && r.Want == Present && r.FromArg == ""
}

// IsExclusion reports whether the relation forbids a direct sibling carrying the label; see ExcludeTag.
func (r Relation) IsExclusion() bool {
	return r.Scope == ScopeSiblings && r.Want == Absent
}

// ValidateRelations reports authoring defects a single builder call cannot see: a Tag shadowing a Kind, a relation naming an undeclared label, a half-specified key match, or a target key no labeled definition carries.
func (s *Schema) ValidateRelations(d *diag.Diagnostics) {
	kinds := map[string]bool{}
	keysOf := map[string]map[string]bool{}
	s.Walk(func(n *Node) {
		if n.KindName != "" {
			kinds[n.KindName] = true
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
	s.Walk(func(n *Node) {
		// Kind is identity-bearing and Tag is not, so one name must not mean both.
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
	})
}

// checkRelation reports one relation whose label or key match cannot resolve against any definition.
func (n *Node) checkRelation(
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
	// Reject combinations no checker implements rather than let them pass silently.
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
