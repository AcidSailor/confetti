package merge

import (
	"slices"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// Outcome reports how a Resolve call settled a contested slot.
type Outcome int

const (
	Refused    Outcome = iota // The earlier value is kept and an Error is reported.
	Overridden                // The returned node won and the other value was discarded.
	Combined                  // Both values survive in the returned node.
)

// Resolve decides what a slot holds when two parts both claim it with
// different values. earlier is the node already in the output tree, later is
// the incoming part node, and declared is the merge kind resolved from the
// slot's schema definitions. A resolver must not modify either argument; it
// returns earlier, later, or a fresh parentless node.
type Resolve func(earlier, later *tree.Node, declared schema.MergeKind) (*tree.Node, Outcome)

// Options configures one Merge call; a nil Resolve uses Declared.
type Options struct {
	Resolve Resolve
}

// Declared applies the slot's declared merge kind: keep the declared winner,
// union a list slot, take the later header of a same-definition section and
// recurse, and keep the later value otherwise.
func Declared(
	earlier, later *tree.Node,
	declared schema.MergeKind,
) (*tree.Node, Outcome) {
	if n, out, ok := resolveDeclared(earlier, later, declared); ok {
		return n, out
	}
	if earlier.Def == later.Def && ident.IsSection(later) {
		return later, Combined
	}
	return later, Overridden
}

// Refuse applies the slot's declared merge kind and refuses everything Declared would resolve by keeping the later value.
func Refuse(
	earlier, later *tree.Node,
	declared schema.MergeKind,
) (*tree.Node, Outcome) {
	if n, out, ok := resolveDeclared(earlier, later, declared); ok {
		return n, out
	}
	return nil, Refused
}

// resolveDeclared settles the slots whose resolution the schema itself sanctions.
func resolveDeclared(
	earlier, later *tree.Node,
	declared schema.MergeKind,
) (*tree.Node, Outcome, bool) {
	switch declared {
	case schema.MergeKeepFirst:
		return earlier, Overridden, true
	case schema.MergeKeepLast:
		return later, Overridden, true
	}
	if n, ok := UnionLists(earlier, later); ok {
		return n, Combined, true
	}
	return nil, Refused, false
}

// KeepFirst keeps the earlier part's value regardless of the declared kind.
func KeepFirst(
	earlier, _ *tree.Node,
	_ schema.MergeKind,
) (*tree.Node, Outcome) {
	return earlier, Overridden
}

// KeepLast keeps the later part's value regardless of the declared kind.
func KeepLast(
	_, later *tree.Node,
	_ schema.MergeKind,
) (*tree.Node, Outcome) {
	return later, Overridden
}

// UnionLists combines two spellings of one list slot into a fresh canonical
// node; ok is false when they are not two spellings of one list slot, which
// is not by itself a conflict.
func UnionLists(earlier, later *tree.Node) (node *tree.Node, ok bool) {
	def := earlier.Def
	if def == nil || def != later.Def ||
		!slices.Equal(earlier.Block, later.Block) {
		return nil, false
	}
	ls := def.ListSpec
	if ls.Arg == "" {
		return nil, false
	}
	kw := ls.Keywords()
	a, aerr := listval.Resolve(earlier.Fields[ls.Arg], ls.Sep, kw)
	b, berr := listval.Resolve(later.Fields[ls.Arg], ls.Sep, kw)
	if aerr != nil || berr != nil {
		return nil, false
	}
	for arg, v := range earlier.Fields {
		if arg != ls.Arg && later.Fields[arg] != v {
			return nil, false
		}
	}
	raw := listval.Canonical(append(a, b...), ls.Sep, kw)
	// Keep the existing spelling when both values resolve to an empty set without a declared None spelling.
	if raw == "" {
		return earlier, true
	}
	n := earlier.CloneValue()
	if n.Fields == nil {
		n.Fields = map[string]string{}
	}
	n.Fields[ls.Arg] = raw
	// Set text last so it renders from the final fields.
	n.Text = def.Render(n.Fields)
	return n, true
}

// Merge folds parts in order into a new tree bound to s without modifying its inputs.
func Merge(
	s *schema.Schema,
	opts Options,
	parts ...*tree.Config,
) (*tree.Config, *diag.Diagnostics) {
	d := diag.New()
	out := tree.NewConfig(s)
	for _, p := range parts {
		if p.Schema != s {
			d.Add(diag.Error, "merge: part uses a different schema")
			return out, d
		}
	}
	resolve := opts.Resolve
	if resolve == nil {
		resolve = Declared
	}
	m := &merger{origin: map[*tree.Node]int{}, resolve: resolve, d: d}
	for i, p := range parts {
		m.level(out.Root, p.Root, i+1)
	}
	return out, d
}

// merger carries one Merge invocation's resolver, origin index, and diagnostics.
type merger struct {
	resolve Resolve
	origin  map[*tree.Node]int // Map each output node to its 1-based source part.
	d       *diag.Diagnostics
}

// level folds one part level into the output level and recurses into shared sections.
func (m *merger) level(outParent, partParent *tree.Node, part int) {
	byIdent := map[ident.Ident]*tree.Node{}
	for _, oc := range outParent.Children {
		id := mergeIdent(oc)
		if _, ok := byIdent[id]; !ok {
			byIdent[id] = oc // Keep the first claimant of a slot.
		}
	}

	for _, pc := range partParent.Children {
		id := mergeIdent(pc)
		oc, ok := byIdent[id]
		if !ok {
			clone := m.cloneSubtree(pc, part)
			outParent.AddChild(clone)
			byIdent[id] = clone
			continue
		}
		declared := declaredKind(oc, pc)
		same := oc.Def == pc.Def && oc.Text == pc.Text &&
			slices.Equal(oc.Block, pc.Block)
		// A non-default section kind resolves even equal headers because it owns the whole stanza.
		if same && (declared == schema.MergeDefault || !ident.IsSection(pc)) {
			if ident.IsSection(pc) {
				m.level(oc, pc, part)
			}
			continue
		}
		if repl := m.slot(oc, pc, declared, part); repl != nil {
			outParent.ReplaceChild(oc, repl)
			byIdent[id] = repl
		}
	}
}

// slot resolves one contested slot in place and returns a replacement node when the later or a fresh value wins whole.
func (m *merger) slot(
	oc, pc *tree.Node,
	declared schema.MergeKind,
	part int,
) *tree.Node {
	node, outcome := m.resolve(oc, pc, declared)
	switch outcome {
	case Refused:
		m.d.Add(diag.Error, "%s: part %d conflicts with part %d (was %q)",
			pc.Path(), part, m.origin[oc], oc.Text)
		return nil
	case Overridden, Combined:
		if node == nil {
			panic("merge: resolver returned a nil node without refusing")
		}
	default:
		panic("merge: resolver returned an unknown Outcome")
	}
	// A fresh node whose text does not render from its fields corrupts every later stage.
	if node != oc && node != pc && node.Def != nil &&
		node.Text != node.Def.Render(node.Fields) {
		m.d.Add(diag.Error,
			"%s: resolver text does not render from its fields",
			pc.Path())
		return nil
	}
	if outcome == Combined {
		if node.Text != oc.Text {
			m.d.Add(diag.Warning, "%s: part %d combines with part %d (was %q)",
				pc.Path(), part, m.origin[oc], oc.Text)
		}
		if node != oc {
			oc.SetValueFrom(node)
		}
		if ident.IsSection(pc) && oc.Def == pc.Def {
			m.level(oc, pc, part)
		}
		return nil
	}
	winner, loser, was := part, m.origin[oc], oc.Text
	if node == oc {
		winner, loser, was = m.origin[oc], part, pc.Text
	}
	m.d.Add(diag.Warning, "%s: part %d overrides part %d (was %q)",
		pc.Path(), winner, loser, was)
	if node == oc {
		return nil
	}
	// The winning value replaces the slot's whole subtree.
	if node == pc {
		return m.cloneSubtree(pc, part)
	}
	m.origin[node] = part
	return node
}

// declaredKind resolves a slot's merge kind, preferring the earlier definition's non-default declaration.
func declaredKind(earlier, later *tree.Node) schema.MergeKind {
	if d := earlier.Def; d != nil && d.Merge != schema.MergeDefault {
		return d.Merge
	}
	if d := later.Def; d != nil {
		return d.Merge
	}
	return schema.MergeDefault
}

// mergeIdent gives each non-keyed ZeroToOne definition one slot per level.
func mergeIdent(n *tree.Node) ident.Ident {
	def := n.Def
	if def != nil && len(def.KeyArgs) == 0 &&
		def.Cardinality == schema.ZeroToOne &&
		ident.CategoryOf(n) != ident.KindedSingle {
		// Use the canonical toggle member so all declared partners share one slot.
		return ident.Ident{Def: def.ToggleCanonical()}
	}
	return ident.Of(n)
}

// cloneSubtree deep-clones a part subtree, recording each clone's origin part.
func (m *merger) cloneSubtree(src *tree.Node, part int) *tree.Node {
	n := src.CloneValue()
	m.origin[n] = part
	for _, c := range src.Children {
		n.AddChild(m.cloneSubtree(c, part))
	}
	return n
}
