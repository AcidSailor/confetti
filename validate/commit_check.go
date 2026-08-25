package validate

import (
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// target identifies a labeled key value.
type target struct{ label, arg, val string }

// relationChecker holds the relation indexes and diagnostic sink.
type relationChecker struct {
	// index holds every declared key value so tree-scope relations resolve against it.
	index map[target]bool
	// present records whether any instance carries each label.
	present map[string]bool
	d       *diag.Diagnostics
}

// CommitCheck validates relations (references, prerequisites, exclusions) against the assembled tree.
func CommitCheck(cfg *schema.Config, d *diag.Diagnostics) {
	// Validate the complete schema, including definitions absent from the configuration.
	if cfg.Schema != nil {
		cfg.Schema.ValidateRelations(d)
	}
	c := relationChecker{
		index:   map[target]bool{},
		present: map[string]bool{},
		d:       d,
	}

	schema.Walk(cfg, c.record)
	schema.Walk(cfg, c.checkRelations)
}

// record indexes the labels and key values one node declares.
func (c relationChecker) record(n *schema.Node) {
	def := n.Def
	if def == nil {
		return
	}
	for _, label := range def.Labels() {
		c.present[label] = true
		for _, arg := range def.KeyArgs {
			c.index[target{label, arg, n.Fields[arg]}] = true
		}
	}
}

// checkRelations validates every relation one node declares against the index.
func (c relationChecker) checkRelations(n *schema.Node) {
	def := n.Def
	if def == nil {
		return
	}
	for _, rel := range def.Relations {
		vals, ok := relValues(n, rel, c.d)
		if !ok {
			continue
		}
		for _, v := range vals {
			c.check(n, rel, v)
		}
	}
}

// relValues returns a presence sentinel or the expanded capture values to match.
func relValues(
	n *schema.Node,
	rel schema.Relation,
	d *diag.Diagnostics,
) ([]string, bool) {
	if rel.FromArg == "" {
		return []string{""}, true
	}
	// Validate list syntax here because CommitCheck can run without ImportCheck.
	if ls := n.Def.ListSpec; ls.Arg == rel.FromArg {
		return listItems(n, ls, d)
	}
	return []string{n.Fields[rel.FromArg]}, true
}

// check reports a diagnostic when one relation value is unsatisfied or in conflict.
func (c relationChecker) check(n *schema.Node, rel schema.Relation, v string) {
	// ValidateRelations rejects every other scope and polarity pairing.
	if rel.IsExclusion() {
		if hit := findSibling(n, rel, v); hit != nil {
			c.d.AddAt(
				n.Line,
				diag.Error,
				"%s: mutually exclusive with %q (line %d) via label %q",
				n.Path(), hit.Text, hit.Line, rel.Label,
			)
		}
		return
	}
	if c.satisfied(rel, v) {
		return
	}
	if rel.FromArg == "" {
		c.d.AddAt(
			n.Line,
			diag.Error,
			"%s: requires a %s instance",
			n.Path(), rel.Label,
		)
		return
	}
	c.d.AddAt(
		n.Line,
		diag.Error,
		"%s: %s %q does not exist",
		n.Path(), rel.Label, v,
	)
}

// satisfied reports whether any node in the tree provides the label and value.
func (c relationChecker) satisfied(rel schema.Relation, v string) bool {
	if rel.FromArg == "" {
		return c.present[rel.Label]
	}
	return c.index[target{rel.Label, rel.TargetKey, v}]
}

// findSibling returns the first direct sibling of n that matches rel, or nil.
func findSibling(n *schema.Node, rel schema.Relation, v string) *schema.Node {
	if n.Parent == nil {
		return nil
	}
	for _, x := range n.Parent.Children {
		if x != n && x.Def != nil && x.Def.HasLabel(rel.Label) &&
			(rel.FromArg == "" || x.Fields[rel.TargetKey] == v) {
			return x
		}
	}
	return nil
}
