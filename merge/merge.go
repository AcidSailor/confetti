package merge

import (
	"slices"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
)

// Resolve decides what a slot holds when two parts both claim it with
// different values. earlier is the node already in the output tree, later is
// the incoming part node, and declared is the merge kind resolved from the
// slot's schema definitions. A resolver must not modify either argument; it
// returns earlier, later, or a fresh parentless node.
type Resolve func(earlier, later *schema.Node, declared schema.MergeKind) (*schema.Node, schema.Outcome)

// Options configures one Merge call; a nil Resolve uses Declared.
type Options struct {
	Resolve Resolve
}

// Declared applies the slot's declared merge kind: keep the declared winner,
// union a list slot, take the later header of a same-definition section and
// recurse, and keep the later value otherwise.
func Declared(
	earlier, later *schema.Node,
	declared schema.MergeKind,
) (*schema.Node, schema.Outcome) {
	if n, out, ok := resolveDeclared(earlier, later, declared); ok {
		return n, out
	}
	if earlier.Def == later.Def && ident.IsSection(later) {
		return later, schema.Combined
	}
	return later, schema.Overridden
}

// Refuse resolves only what the schema sanctions and refuses every other contested slot, including same-definition sections whose headers differ.
func Refuse(
	earlier, later *schema.Node,
	declared schema.MergeKind,
) (*schema.Node, schema.Outcome) {
	if n, out, ok := resolveDeclared(earlier, later, declared); ok {
		return n, out
	}
	return nil, schema.Refused
}

// resolveDeclared settles the slots whose resolution the schema itself sanctions.
func resolveDeclared(
	earlier, later *schema.Node,
	declared schema.MergeKind,
) (*schema.Node, schema.Outcome, bool) {
	switch declared {
	case schema.MergeKeepFirst:
		return earlier, schema.Overridden, true
	case schema.MergeKeepLast:
		return later, schema.Overridden, true
	case schema.MergeCustom:
		if fn := declaredStrategy(earlier, later).Func; fn != nil {
			n, out := fn(earlier, later)
			return n, out, true
		}
	}
	if n, ok := UnionLists(earlier, later); ok {
		return n, schema.Combined, true
	}
	return nil, schema.Refused, false
}

// KeepFirst keeps the earlier part's value regardless of the declared kind.
func KeepFirst(
	earlier, _ *schema.Node,
	_ schema.MergeKind,
) (*schema.Node, schema.Outcome) {
	return earlier, schema.Overridden
}

// KeepLast keeps the later part's value regardless of the declared kind.
func KeepLast(
	_, later *schema.Node,
	_ schema.MergeKind,
) (*schema.Node, schema.Outcome) {
	return later, schema.Overridden
}

// UnionLists combines two spellings of one list slot into a canonical node,
// or earlier itself when both resolve to an empty set with no declared None
// spelling; ok is false when they are not two spellings of one list slot,
// which is not by itself a conflict.
func UnionLists(earlier, later *schema.Node) (node *schema.Node, ok bool) {
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
	parts ...*schema.Config,
) (*schema.Config, *diag.Diagnostics) {
	d := diag.New()
	out := schema.NewConfig(s)
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
	m := &merger{
		schema:  s,
		origin:  map[*schema.Node]int{},
		resolve: resolve,
		d:       d,
	}
	for i, p := range parts {
		m.level(out.Root, p.Root, i+1)
	}
	return out, d
}

// merger carries one Merge invocation's resolver, origin index, and diagnostics.
type merger struct {
	schema  *schema.Schema
	resolve Resolve
	origin  map[*schema.Node]int // Map each output node to its 1-based source part.
	d       *diag.Diagnostics
}

// level folds one part level into the output level and recurses into shared sections.
func (m *merger) level(outParent, partParent *schema.Node, part int) {
	byIdent := map[ident.Ident]*schema.Node{}
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
		declared := declaredStrategy(oc, pc).Kind
		same := oc.Def == pc.Def && oc.Text == pc.Text &&
			slices.Equal(oc.Block, pc.Block)
		// A non-default section kind resolves even equal headers because it owns the whole stanza.
		if same && (declared == schema.MergeDefault || !ident.IsSection(pc)) {
			if ident.IsSection(pc) {
				m.level(oc, pc, part)
			}
			continue
		}
		if repl := m.slot(outParent, oc, pc, declared, part); repl != nil {
			outParent.ReplaceChild(oc, repl)
			byIdent[id] = repl
		}
	}
}

// slot resolves one contested slot in place and returns a replacement node when the later or a fresh value wins whole.
func (m *merger) slot(
	outParent, oc, pc *schema.Node,
	declared schema.MergeKind,
	part int,
) *schema.Node {
	node, outcome := m.resolve(oc, pc, declared)
	switch outcome {
	case schema.Refused:
		m.d.Add(diag.Error, "%s: part %d conflicts with part %d (was %q)",
			pc.Path(), part, m.origin[oc], oc.Text)
		return nil
	case schema.Overridden, schema.Combined:
		if node == nil {
			panic("merge: resolver returned a nil node without refusing")
		}
	default:
		panic("merge: resolver returned an unknown Outcome")
	}
	if node != oc && node != pc && !m.freshNodeValid(outParent, oc, node, pc) {
		return nil
	}
	if outcome == schema.Combined {
		// Merging children across definitions produces a tree its own schema cannot re-parse.
		prevDef := oc.Def
		if (oc.Def != pc.Def || node.Def != oc.Def) &&
			(ident.IsSection(oc) || ident.IsSection(pc)) {
			m.d.Add(diag.Error,
				"%s: resolver combined sections bound to different definitions",
				pc.Path())
			return nil
		}
		switch {
		case node.Text != oc.Text:
			m.d.Add(diag.Warning, "%s: part %d combines with part %d (was %q)",
				pc.Path(), part, m.origin[oc], oc.Text)
			m.origin[oc] = part
		// Handing back the earlier node itself leaves a leaf's later value in no node at all.
		case node == oc && !ident.IsSection(pc) && pc.Text != oc.Text:
			m.d.Add(diag.Warning,
				"%s: part %d combined without part %d's value (was %q)",
				pc.Path(), m.origin[oc], part, pc.Text)
		}
		if node != oc {
			oc.SetValueFrom(node)
		}
		if ident.IsSection(pc) && prevDef == pc.Def {
			m.level(oc, pc, part)
		}
		return nil
	}
	// A fresh node carries no children, so letting it win a section would erase both stanzas.
	if node != oc && node != pc &&
		(ident.IsSection(oc) || ident.IsSection(pc)) {
		m.d.Add(diag.Error,
			"%s: resolver replaced a section with a childless fresh node",
			pc.Path())
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

// freshNodeValid reports whether a resolver-constructed node keeps its identity through later stages, reporting an Error when it cannot.
func (m *merger) freshNodeValid(outParent, oc, node, pc *schema.Node) bool {
	// Contested slots always carry definitions, so a definition-free fresh node can only lose identity.
	if node.Def == nil {
		m.d.Add(diag.Error,
			"%s: resolver returned a node without a definition",
			pc.Path())
		return false
	}
	// Adopting a node that another tree still owns would splice two trees together.
	if node.Parent != nil {
		m.d.Add(diag.Error,
			"%s: resolver returned a node that is already in a tree",
			pc.Path())
		return false
	}
	// A different pairing identity would leave the slot map naming a node that no longer fills it.
	if mergeIdent(node) != mergeIdent(oc) {
		m.d.Add(diag.Error,
			"%s: resolver changed the slot's identity",
			pc.Path())
		return false
	}
	// A text that does not render from its fields corrupts every later stage.
	if node.Text != node.Def.Render(node.Fields) {
		m.d.Add(diag.Error,
			"%s: resolver text does not render from its fields",
			pc.Path())
		return false
	}
	// The text must bind its own definition when parsed again, or the merged tree and its round-trip disagree on identity.
	cands := m.schema.Roots
	if outParent.Def != nil {
		cands = outParent.Def.Children
	}
	if def, _, ok := schema.MatchChild(cands, node.Text); !ok ||
		def != node.Def {
		m.d.Add(diag.Error,
			"%s: resolver text does not bind its own definition",
			pc.Path())
		return false
	}
	return true
}

// declaredStrategy resolves a slot's merge strategy, preferring the earlier definition's non-default declaration.
func declaredStrategy(earlier, later *schema.Node) schema.MergeStrategy {
	if d := earlier.Def; d != nil && d.Merge.Kind != schema.MergeDefault {
		return d.Merge
	}
	if d := later.Def; d != nil {
		return d.Merge
	}
	return schema.MergeStrategy{}
}

// mergeIdent gives each non-keyed ZeroToOne definition one slot per level.
func mergeIdent(n *schema.Node) ident.Ident {
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
func (m *merger) cloneSubtree(src *schema.Node, part int) *schema.Node {
	n := src.CloneValue()
	m.origin[n] = part
	for _, c := range src.Children {
		n.AddChild(m.cloneSubtree(c, part))
	}
	return n
}
