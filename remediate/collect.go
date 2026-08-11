package remediate

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// levelKey sorts creates by ascending rank and sequence, and removes by descending rank and sequence.
type levelKey [3]int

// pathKey stores the baseline declaration order from outer sections to the operation.
type pathKey []levelKey

func (a pathKey) compare(b pathKey) int {
	for i := range min(len(a), len(b)) {
		if c := slices.Compare(a[i][:], b[i][:]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

// op is one pending remediation change plus everything ordering needs.
type op struct {
	node    *tree.Node // built remediation node (OpAdd subtree / OpRemove leaf / OpModify leaf)
	pre     *tree.Node // negate half of a replace pair, emitted immediately before node
	src     *tree.Node // originating node: intended for creates, running for removes
	runSrc  *tree.Node // running counterpart for Modify/Replace (nil otherwise)
	flipRun *tree.Node // superseded toggle partner whose removed subtree also drives dependencies
	action  graph.Action
	// secs contains kept ancestor sections from outermost to innermost; scheduling compares only pointer identity.
	secs []*tree.Node
	key  pathKey // baseline order
}

// differ carries one Diff invocation's shared state through collect, derive, and schedule.
type differ struct {
	running, intended *tree.Config
	order             map[*schema.Node]int
	p                 diag.Policy
	d                 *diag.Diagnostics
	ops               []op
	g                 *graph.Graph
	why               edgeReasons
}

// collect pairs running and intended children and appends flat operations for later scheduling and materialization.
func (dv *differ) collect(
	runParent, intParent *tree.Node,
	secs []*tree.Node,
	prefix pathKey,
) {
	d := dv.d
	runByIdent := indexByIdent(runParent.Children, d)
	intByIdent := indexByIdent(intParent.Children, d)

	var intents []createIntent
	for _, ic := range intParent.Children {
		rc, ok := runByIdent[ident.Of(ic)]
		switch {
		case !ok:
			intents = append(intents, createIntent{src: ic, kind: ckAdd})
		case ic.Def != rc.Def && (ident.IsSection(ic) || ident.IsSection(rc)):
			intents = append(intents,
				createIntent{src: ic, run: rc, kind: ckReplace})
		case ident.IsSection(ic):
			intents = append(
				intents,
				createIntent{src: ic, run: rc, kind: ckSection},
			)
		case ic.Text != rc.Text ||
			!slices.Equal(ic.Block, rc.Block):
			if ic.Def != nil && ic.Def.Idempotent {
				if ci, ok := listOrModify(ic, rc); ok {
					intents = append(intents, ci)
				}
			} else {
				intents = append(
					intents,
					createIntent{src: ic, run: rc, kind: ckReplace},
				)
			}
		}
		// Equal leaves and unchanged keyed leaves need no operation.
	}

	var removes []*tree.Node
	for _, rc := range runParent.Children {
		if _, ok := intByIdent[ident.Of(rc)]; !ok {
			removes = append(removes, rc)
		}
	}
	removes, flips := dropToggles(intents, removes, d)

	for i, ci := range intents {
		k := append(slices.Clone(prefix), levelKey{0, dv.order[ci.src.Def], i})
		switch ci.kind {
		case ckAdd:
			dv.ops = append(dv.ops, op{
				node:    buildAdd(ci.src),
				src:     ci.src,
				flipRun: flips[ci.src.Def],
				action:  graph.Add,
				secs:    secs,
				key:     k,
			})
		case ckModify:
			dv.ops = append(dv.ops, op{
				node: cloneNode(ci.src, tree.OpModify),
				src:  ci.src, runSrc: ci.run,
				action: graph.Modify, secs: secs, key: k,
			})
		case ckListDelta:
			ls := ci.src.Def.ListSpec
			var pre, node *tree.Node
			if len(ci.removeItems) > 0 {
				pre = deltaLeaf(ls.RemoveTmpl, ci.src.Fields,
					ls, ci.removeItems)
			}
			if len(ci.addItems) > 0 {
				node = deltaLeaf(ls.AddTmpl, ci.src.Fields,
					ls, ci.addItems)
			}
			if node == nil {
				node, pre = pre, nil // A pure removal emits one line.
			}
			dv.ops = append(dv.ops, op{
				node: node, pre: pre,
				src: ci.src, runSrc: ci.run,
				action: graph.Modify, secs: secs, key: k,
			})
		case ckSection:
			// Emit a changed non-key header before its child operations, then recurse into the section.
			if ci.src.Text != ci.run.Text {
				dv.ops = append(dv.ops, op{
					node: cloneNode(ci.src, tree.OpModify),
					src:  ci.src, runSrc: ci.run,
					action: graph.Modify, secs: secs, key: k,
				})
			}
			child := append(slices.Clone(secs), ci.src)
			dv.collect(ci.run, ci.src, child, k)
		case ckReplace:
			// Refuse replacement when either paired definition is protected because replacement includes deletion.
			runProtected := ci.run.Def != nil && ci.run.Def.Protected
			srcProtected := ci.src.Def != nil && ci.src.Def.Protected
			if runProtected || srcProtected {
				d.Add(diag.Error, "%s: refusing to replace protected %q",
					ci.run.Path(), ci.run.Text)
				continue
			}
			// Refuse replacement of a running EmptyOnRemove section because it has no header negation form.
			if ci.run.Def != nil && ci.run.Def.EmptyOnRemove {
				d.Add(
					diag.Error,
					"%s: refusing to replace EmptyOnRemove section %q: it has no removal form",
					ci.run.Path(),
					ci.run.Text,
				)
				continue
			}
			dv.ops = append(dv.ops, op{
				node: buildAdd(ci.src), pre: buildRemove(ci.run, d),
				src: ci.src, runSrc: ci.run,
				action: graph.Replace, secs: secs, key: k,
			})
		}
	}
	for i, rc := range removes {
		k := append(slices.Clone(prefix), levelKey{1, -dv.order[rc.Def], -i})
		if def := rc.Def; def != nil && def.EmptyOnRemove {
			dv.expandRemove(rc, secs, k)
			continue
		}
		// Refuse deletion of a protected node or descendant in both policies after toggle flips are removed.
		if p := protectedIn(rc); p != nil {
			d.Add(diag.Error, "%s: refusing to delete protected %q",
				p.Path(), p.Text)
			continue
		}
		dv.ops = append(dv.ops, op{
			node: buildRemove(rc, d), src: rc,
			action: graph.Remove, secs: secs, key: k,
		})
	}
}

// expandRemove converts each child of an EmptyOnRemove section into a separate removal while retaining the header as context.
func (dv *differ) expandRemove(
	rc *tree.Node,
	secs []*tree.Node,
	prefix pathKey,
) {
	d := dv.d
	// Report an EmptyOnRemove definition without child grammar because it cannot converge.
	if len(rc.Def.Children) == 0 {
		d.Add(
			diag.Error,
			"%s: EmptyOnRemove def has no child grammar; removal cannot converge",
			rc.Path(),
		)
		return
	}
	child := append(slices.Clone(secs), rc)
	for j, cc := range rc.Children {
		k := append(slices.Clone(prefix), levelKey{1, -dv.order[cc.Def], -j})
		if def := cc.Def; def != nil && def.EmptyOnRemove {
			dv.expandRemove(cc, child, k)
			continue
		}
		if p := protectedIn(cc); p != nil {
			d.Add(diag.Error, "%s: refusing to delete protected %q",
				p.Path(), p.Text)
			continue
		}
		dv.ops = append(dv.ops, op{
			node: buildRemove(cc, d), src: cc,
			action: graph.Remove, secs: child, key: k,
		})
	}
}

// protectedIn returns the first protected node in the subtree, including n.
func protectedIn(n *tree.Node) *tree.Node {
	if def := n.Def; def != nil && def.Protected {
		return n
	}
	for _, c := range n.Children {
		if p := protectedIn(c); p != nil {
			return p
		}
	}
	return nil
}

// secTexts renders kept section headers as the grouping key shared by graph, compare, and the change log.
func secTexts(secs []*tree.Node) []string {
	out := make([]string, len(secs))
	for i, s := range secs {
		out[i] = s.Text
	}
	return out
}

// opViews builds the plain-data graph.Op views handed to derivation and hooks.
func opViews(ops []op) []graph.Op {
	vs := make([]graph.Op, len(ops))
	for i, o := range ops {
		v := graph.Op{
			Index:  i,
			Action: o.action,
			Text:   o.node.Text,
			Path:   secTexts(o.secs),
		}
		if def := o.src.Def; def != nil {
			v.Kind = def.KindName
			if len(def.KeyArgs) > 0 {
				v.Key = ident.KeyValue(o.src)
			}
		}
		vs[i] = v
	}
	return vs
}

// opPath renders kept sections and the operation line before materialization assigns parents.
func opPath(o op) string {
	return strings.Join(append(secTexts(o.secs), o.node.Text), " / ")
}
