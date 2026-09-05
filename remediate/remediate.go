package remediate

import (
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// Cycle selects how Diff handles an ordering cycle.
type Cycle int

const (
	Abort Cycle = iota // Report an Error and emit nothing.
	Break              // Drop the greatest-keyed edge in the cycle, report a Warning, continue.
)

// Options configures Diff; the zero value aborts on cycles and has no baseline.
type Options struct {
	Cycle Cycle
	// Baseline holds device-provided objects that satisfy Requires without appearing in either configuration; negating one is an Error.
	Baseline *schema.Config
}

// Result contains an operation-tagged remediation tree and its change log in emission order.
type Result struct {
	Tree    *schema.Config
	Changes []Change
}

// Empty reports whether the remediation contains no change operations.
func (r *Result) Empty() bool {
	empty := true
	schema.Walk(r.Tree, func(n *schema.Node) {
		if isChange(n) {
			empty = false
		}
	})
	return empty
}

// isChange reports whether a node carries a change rather than section scaffolding.
func isChange(n *schema.Node) bool {
	switch n.Op {
	case schema.OpNone, schema.OpSection:
		// Section scaffolding and untagged nodes are not changes.
		return false
	default:
		// Treat unknown future operations as changes.
		return true
	}
}

// Diff returns the remediation from running to intended without modifying any input; running, intended, and any baseline must use the same schema.
func Diff(
	running, intended *schema.Config,
	o Options,
) (*Result, *diag.Diagnostics) {
	d := diag.New()
	out := schema.NewConfig(intended.Schema)
	if running.Schema != intended.Schema {
		d.Add(
			diag.Error,
			"remediate: running and intended use different schemas",
		)
		return &Result{Tree: out}, d
	}
	if o.Baseline != nil && o.Baseline.Schema != intended.Schema {
		d.Add(
			diag.Error,
			"remediate: baseline and intended use different schemas",
		)
		return &Result{Tree: out}, d
	}
	dv := &differ{
		running: running, intended: intended,
		order: buildOrderIndex(intended.Schema),
		cycle: o.Cycle, d: d,
	}
	// Index the baseline once; both the negation guard and Requires survivors read it.
	if o.Baseline != nil {
		dv.provided = labelResources(o.Baseline)
	}
	dv.collect(running.Root, intended.Root, nil, nil)
	dv.checkBaselineRemovals()
	dv.buildGraph()
	seq := dv.schedule()
	if seq == nil {
		return &Result{Tree: out}, d
	}
	materialize(seq, dv.ops, out)
	return &Result{Tree: out, Changes: changesFrom(seq, dv.ops)}, d
}

type createKind int

const (
	ckAdd createKind = iota
	ckSection
	ckModify
	ckReplace   // paired leaf, same identity, new value: negate old + add new
	ckListDelta // paired list slot: emit add/remove delta lines
)

type createIntent struct {
	src  *schema.Node // intended node
	run  *schema.Node // running counterpart (nil for ckAdd)
	kind createKind
	// ckListDelta only: canonical item subsets to add/remove.
	addItems, removeItems []string
}

// dropToggles removes negations for declared toggle partners and maps each added definition to its replaced running node.
func dropToggles(
	intents []createIntent,
	removes []*schema.Node,
	d *diag.Diagnostics,
) ([]*schema.Node, map[*schema.Def]*schema.Node) {
	added := map[*schema.Def]bool{}
	for _, ci := range intents {
		if ci.kind == ckAdd && ci.src.Def != nil {
			added[ci.src.Def] = true
		}
	}
	if len(added) == 0 {
		return removes, nil
	}
	flips := map[*schema.Def]*schema.Node{}
	kept := removes[:0:0]
	for _, rc := range removes {
		// The added sibling can be any member of the removed definition's toggle group.
		var addedSib *schema.Def
		if def := rc.Def; def != nil {
			for _, m := range def.ToggleGroup {
				if m != def && added[m] {
					addedSib = m
					break
				}
			}
		}
		if addedSib != nil {
			// Keep the removal when a toggle member has a protected descendant because the flip would delete that descendant.
			protectedChild := false
			for _, c := range rc.Children {
				if protectedIn(c) != nil {
					protectedChild = true
					break
				}
			}
			if !protectedChild {
				// Pair the first superseded member and warn about additional members without emitting unsafe trailing negations.
				if first, dup := flips[addedSib]; dup {
					d.AddAt(
						rc.Line,
						diag.Warning,
						"%s: running carries multiple members of toggle group; %q also superseded (change log pairs %q)",
						rc.Path(),
						rc.Text,
						first.Text,
					)
				} else {
					flips[addedSib] = rc
				}
				continue
			}
		}
		kept = append(kept, rc)
	}
	return kept, flips
}

// cloneNode copies an intended node without aliasing source state and preserves its definition for canonical rendering.
func cloneNode(src *schema.Node, op schema.Op) *schema.Node {
	n := src.CloneValue()
	n.Op = op
	return n
}

// buildAdd deep-rebuilds an intended subtree as OpAdd nodes.
func buildAdd(src *schema.Node) *schema.Node {
	n := cloneNode(src, schema.OpAdd)
	for _, c := range src.Children {
		n.AddChild(buildAdd(c))
	}
	return n
}

// buildRemove creates one definition-free OpRemove leaf; section header negation removes its complete subtree.
func buildRemove(src *schema.Node, d *diag.Diagnostics) *schema.Node {
	rendered := src.Text
	if def := src.Def; def != nil {
		rendered = def.Render(src.Fields)
	} else {
		d.Add(diag.Warning, "%s: negating unmatched line", src.Path())
	}
	n := schema.NewNode(negateLine(src.Def, src.Fields, rendered))
	n.Op = schema.OpRemove
	return n
}
