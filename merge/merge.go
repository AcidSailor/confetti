package merge

import (
	"slices"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
)

// Resolve returns earlier, later, or a fresh parentless node for a contested slot without mutating either input.
type Resolve func(earlier, later *schema.Node, declared schema.MergeStrategy) (*schema.Node, schema.Outcome)

// Options configures one Merge call; a nil Resolve uses Declared.
type Options struct {
	Resolve Resolve
}

// Declared applies the schema strategy, unions lists, merges matching sections, and otherwise keeps the later value.
func Declared(
	earlier, later *schema.Node,
	declared schema.MergeStrategy,
) (*schema.Node, schema.Outcome) {
	if n, out, ok := resolveDeclared(earlier, later, declared); ok {
		return n, out
	}
	if earlier.Def == later.Def && ident.IsSection(later) {
		return later, schema.Combined
	}
	return later, schema.Overridden
}

// Refuse resolves schema strategies and list unions, and rejects other conflicts.
func Refuse(
	earlier, later *schema.Node,
	declared schema.MergeStrategy,
) (*schema.Node, schema.Outcome) {
	n, out, _ := resolveDeclared(earlier, later, declared)
	return n, out
}

// resolveDeclared applies explicit schema strategies and list union.
func resolveDeclared(
	earlier, later *schema.Node,
	declared schema.MergeStrategy,
) (*schema.Node, schema.Outcome, bool) {
	switch declared.Kind {
	case schema.MergeKeepFirst:
		return earlier, schema.Overridden, true
	case schema.MergeKeepLast:
		return later, schema.Overridden, true
	case schema.MergeCustom:
		if declared.Func != nil {
			n, out := declared.Func(earlier, later)
			return n, out, true
		}
	}
	if n, ok := UnionLists(earlier, later); ok {
		return n, schema.Combined, true
	}
	return nil, schema.Refused, false
}

// UnionLists returns a canonical union and reports whether both nodes are spellings of one list slot.
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
	if raw == "" {
		return earlier, true
	}
	n := earlier.CloneValue()
	if n.Fields == nil {
		n.Fields = map[string]string{}
	}
	n.Fields[ls.Arg] = raw
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

type merger struct {
	schema  *schema.Schema
	resolve Resolve
	origin  map[*schema.Node]int // 1-based source part for each output node.
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
		declared := declaredStrategy(oc, pc)
		// A non-default section strategy owns the complete stanza, even when headers match.
		if oc.SameValue(pc) &&
			(declared.Kind == schema.MergeDefault || !ident.IsSection(pc)) {
			if ident.IsSection(pc) {
				m.level(oc, pc, part)
			}
			continue
		}
		if repl := m.slot(outParent, oc, pc, id, declared, part); repl != nil {
			outParent.ReplaceChild(oc, repl)
			byIdent[id] = repl
		}
	}
}

// slot resolves a contested slot and returns a whole-subtree replacement when needed.
func (m *merger) slot(
	outParent, oc, pc *schema.Node,
	id ident.Ident,
	declared schema.MergeStrategy,
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
	fresh := node != oc && node != pc
	section := ident.IsSection(oc) || ident.IsSection(pc)
	if fresh && !m.freshNodeValid(outParent, node, pc, id) {
		return nil
	}
	if outcome == schema.Combined {
		m.combine(oc, pc, node, section, part)
		return nil
	}
	// A fresh node carries no children, so letting it win a section would erase both stanzas.
	if fresh && section {
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
	if node == pc {
		return m.cloneSubtree(pc, part)
	}
	m.origin[node] = part
	return node
}

// combine applies a Combined outcome and merges children for one shared section definition.
func (m *merger) combine(oc, pc, node *schema.Node, section bool, part int) {
	// Children from different definitions cannot share a section header.
	prevDef := oc.Def
	if section && (oc.Def != pc.Def || node.Def != oc.Def) {
		m.d.Add(diag.Error,
			"%s: resolver combined sections bound to different definitions",
			pc.Path())
		return
	}
	switch {
	case node.Text != oc.Text:
		m.d.Add(diag.Warning, "%s: part %d combines with part %d (was %q)",
			pc.Path(), part, m.origin[oc], oc.Text)
		m.origin[oc] = part
	// Returning the earlier leaf omits the later value.
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
}

// freshNodeValid checks the identity and ownership of a resolver-built node.
func (m *merger) freshNodeValid(
	outParent, node, pc *schema.Node,
	id ident.Ident,
) bool {
	// A definition-free node cannot retain a contested slot's identity.
	if node.Def == nil {
		m.d.Add(diag.Error,
			"%s: resolver returned a node without a definition",
			pc.Path())
		return false
	}
	// An owned node would splice two trees together.
	if node.Parent != nil {
		m.d.Add(diag.Error,
			"%s: resolver returned a node that is already in a tree",
			pc.Path())
		return false
	}
	// The replacement must keep the contested slot identity.
	if mergeIdent(node) != id {
		m.d.Add(diag.Error,
			"%s: resolver changed the slot's identity",
			pc.Path())
		return false
	}
	// Text and fields must describe the same value.
	if node.Text != node.Def.Render(node.Fields) {
		m.d.Add(diag.Error,
			"%s: resolver text does not render from its fields",
			pc.Path())
		return false
	}
	// Re-parsing must select the same definition.
	cands := m.schema.Roots
	if outParent.Def != nil {
		cands = outParent.Def.Children
	}
	if _, ok := schema.BindsDef(cands, node.Def, node.Text); !ok {
		m.d.Add(diag.Error,
			"%s: resolver text does not bind its own definition",
			pc.Path())
		return false
	}
	return true
}

// declaredStrategy prefers the earlier definition's non-default strategy.
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
