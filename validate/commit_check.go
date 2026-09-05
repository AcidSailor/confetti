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
	// held preserves claim order with baseline claims first.
	held map[holder][]claim
	// claims records the claim each node made so checking never resolves a list twice.
	claims map[*schema.Node]claim
	// baseline distinguishes device-provided claims in diagnostics.
	baseline map[*schema.Node]bool
	d        *diag.Diagnostics
}

// CommitCheck validates relations and exclusive-name collisions against the assembled tree; a nil baseline provides no targets and holds no names.
func CommitCheck(cfg, baseline *schema.Config, d *diag.Diagnostics) {
	if baseline != nil && baseline.Schema != cfg.Schema {
		d.Add(
			diag.Error,
			"validate: baseline and configuration use different schemas",
		)
		// Drop the unusable baseline but still check the tree the caller asked about.
		baseline = nil
	}
	// Report an invalid baseline separately from the configuration.
	if baseline != nil && baseline.Root == nil {
		d.Add(diag.Error, "validate: baseline has no parsed nodes")
		baseline = nil
	}
	// Validate the complete schema, including definitions absent from the configuration.
	if cfg.Schema != nil {
		cfg.Schema.ValidateRelations(d)
	}
	c := relationChecker{
		index:    map[target]bool{},
		present:  map[string]bool{},
		held:     map[holder][]claim{},
		claims:   map[*schema.Node]claim{},
		baseline: map[*schema.Node]bool{},
		d:        d,
	}

	// Baseline nodes are relation targets and name holders; their own relations are never checked.
	if baseline != nil {
		schema.Walk(baseline, c.recordBaseline)
	}
	schema.Walk(cfg, c.recordConfig)
	schema.Walk(cfg, c.checkRelations)
	schema.Walk(cfg, c.checkExclusive)
}

// recordBaseline marks a device-provided node and indexes what it declares.
func (c relationChecker) recordBaseline(n *schema.Node) {
	c.baseline[n] = true
	c.record(n, true)
}

// recordConfig indexes what one node of the caller's configuration declares.
func (c relationChecker) recordConfig(n *schema.Node) { c.record(n, false) }

// record indexes the labels, key values, and exclusive name one node declares.
func (c relationChecker) record(n *schema.Node, fromBaseline bool) {
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
	if cl, ok := c.claimOf(n, fromBaseline); ok {
		c.claims[n] = cl
		c.held[cl.holder] = append(c.held[cl.holder], cl)
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
