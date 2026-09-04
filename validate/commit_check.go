package validate

import (
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// target identifies a labeled key value.
type target struct{ label, arg, val string }

// holder identifies one exclusive name under a Namespace or Kind.
type holder struct{ label, key string }

// relationChecker holds the relation indexes and diagnostic sink.
type relationChecker struct {
	// index holds every declared key value so tree-scope relations resolve against it.
	index map[target]bool
	// present records whether any instance carries each label.
	present map[string]bool
	// held maps each exclusive name to its first holder so a second claim is a collision.
	held map[holder]*schema.Node
	// baseline marks holders that the device provides.
	baseline map[*schema.Node]bool
	d        *diag.Diagnostics
}

// CommitCheck validates relations (references, prerequisites, exclusions) against the assembled tree; a nil baseline provides no targets.
func CommitCheck(cfg, baseline *schema.Config, d *diag.Diagnostics) {
	if baseline != nil && baseline.Schema != cfg.Schema {
		d.Add(
			diag.Error,
			"validate: baseline and configuration use different schemas",
		)
		// Drop the unusable baseline but still check the tree the caller asked about.
		baseline = nil
	}
	// Validate the complete schema, including definitions absent from the configuration.
	if cfg.Schema != nil {
		cfg.Schema.ValidateRelations(d)
	}
	c := relationChecker{
		index:    map[target]bool{},
		present:  map[string]bool{},
		held:     map[holder]*schema.Node{},
		baseline: map[*schema.Node]bool{},
		d:        d,
	}

	// Baseline nodes are relation targets only; their own relations are never checked.
	if baseline != nil {
		schema.Walk(baseline, func(n *schema.Node) {
			c.baseline[n] = true
			c.record(n)
		})
	}
	schema.Walk(cfg, c.record)
	schema.Walk(cfg, c.checkRelations)
	schema.Walk(cfg, c.checkExclusive)
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
	if h, ok := heldName(n); ok && c.held[h] == nil {
		c.held[h] = n
	}
}

// heldName returns the exclusive name one node holds under its ExclusiveLabel.
func heldName(n *schema.Node) (holder, bool) {
	def := n.Def
	if def == nil || len(def.KeyArgs) == 0 || def.ExclusiveLabel() == "" {
		return holder{}, false
	}
	return holder{def.ExclusiveLabel(), fieldKey(n, def.ExclusiveArgs())}, true
}

// fieldKey joins the values of args into one comparable key.
func fieldKey(n *schema.Node, args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = n.Fields[a]
	}
	return strings.Join(parts, "\x00")
}

// checkExclusive reports a node whose exclusive name another object already holds.
func (c relationChecker) checkExclusive(n *schema.Node) {
	h, ok := heldName(n)
	if !ok {
		return
	}
	first := c.held[h]
	// Re-entering the same object is idempotent, not a collision.
	if first == n || first.Def == n.Def &&
		fieldKey(first, n.Def.KeyArgs) == fieldKey(n, n.Def.KeyArgs) {
		return
	}
	name := strings.ReplaceAll(h.key, "\x00", ",")
	if c.baseline[first] {
		c.d.AddAt(n.Line, diag.Error,
			"%s: name %q under label %q is already held by baseline %q",
			n.Path(), name, h.label, first.Text)
		return
	}
	c.d.AddAt(n.Line, diag.Error,
		"%s: name %q under label %q is already held by %q (line %d)",
		n.Path(), name, h.label, first.Text, first.Line)
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
