package remediate

import (
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// Result contains an operation-tagged remediation tree and its change log in emission order.
type Result struct {
	Tree    *tree.Config
	Changes []Change
}

// Empty reports whether the remediation contains no change operations.
func (r *Result) Empty() bool {
	empty := true
	tree.Walk(r.Tree, func(n *tree.Node) {
		switch n.Op {
		case tree.OpNone, tree.OpSection:
			// Section scaffolding and untagged nodes are not changes.
		default:
			// Treat unknown future operations as changes.
			empty = false
		}
	})
	return empty
}

// Diff returns the remediation from running to intended without modifying either input; both trees must use the same schema.
func Diff(
	running, intended *tree.Config,
	p diag.Policy,
) (*Result, *diag.Diagnostics) {
	d := diag.New()
	out := tree.NewConfig(intended.Schema)
	if running.Schema != intended.Schema {
		d.Add(
			diag.Error,
			"remediate: running and intended use different schemas",
		)
		return &Result{Tree: out}, d
	}
	dv := &differ{
		running: running, intended: intended,
		order: buildOrderIndex(intended.Schema),
		p:     p, d: d,
	}
	dv.collect(running.Root, intended.Root, nil, nil)
	dv.buildGraph()
	seq := dv.schedule()
	if seq == nil { // A strict cycle prevents artifact output.
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
	src  *tree.Node // intended node
	run  *tree.Node // running counterpart (nil for ckAdd)
	kind createKind
	// ckListDelta only: canonical item subsets to add/remove.
	addItems, removeItems []string
}

// dropToggles removes negations for declared toggle partners and maps each added definition to its replaced running node.
func dropToggles(
	intents []createIntent,
	removes []*tree.Node,
	d *diag.Diagnostics,
) ([]*tree.Node, map[*schema.Node]*tree.Node) {
	added := map[*schema.Node]bool{}
	for _, ci := range intents {
		if ci.kind == ckAdd && ci.src.Def != nil {
			added[ci.src.Def] = true
		}
	}
	if len(added) == 0 {
		return removes, nil
	}
	flips := map[*schema.Node]*tree.Node{}
	kept := removes[:0:0]
	for _, rc := range removes {
		// The added sibling can be any member of the removed definition's toggle group.
		var addedSib *schema.Node
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
func cloneNode(src *tree.Node, op tree.Op) *tree.Node {
	n := src.CloneValue()
	n.Op = op
	return n
}

// buildAdd deep-rebuilds an intended subtree as OpAdd nodes.
func buildAdd(src *tree.Node) *tree.Node {
	n := cloneNode(src, tree.OpAdd)
	for _, c := range src.Children {
		n.AddChild(buildAdd(c))
	}
	return n
}

// buildRemove creates one definition-free OpRemove leaf; section header negation removes its complete subtree.
func buildRemove(src *tree.Node, d *diag.Diagnostics) *tree.Node {
	rendered := src.Text
	if def := src.Def; def != nil {
		rendered = def.Render(src.Fields)
	} else {
		d.Add(diag.Warning, "%s: negating unmatched line", src.Path())
	}
	n := tree.NewNode(negateLine(src.Def, src.Fields, rendered))
	n.Op = tree.OpRemove
	return n
}
