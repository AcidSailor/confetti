package merge

import (
	"maps"
	"slices"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// Merge folds parts in order into a new tree bound to s without modifying its inputs.
func Merge(
	s *schema.Schema,
	policy diag.Policy,
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
	m := &merger{origin: map[*tree.Node]int{}, policy: policy, d: d}
	for i, p := range parts {
		m.level(out.Root, p.Root, i+1)
	}
	return out, d
}

// merger carries one Merge invocation's policy, origin index, and diagnostics.
type merger struct {
	policy diag.Policy
	origin map[*tree.Node]int // Map each output node to its 1-based source part.
	d      *diag.Diagnostics
}

// level folds one part level into the output level and recurses into shared sections.
func (m *merger) level(outParent, partParent *tree.Node, part int) {
	policy, origin, d := m.policy, m.origin, m.d
	verb := "overrides"
	if policy.Strict {
		verb = "conflicts with"
	}
	// conflict reports one slot claimed twice with different values.
	conflict := func(oc, pc *tree.Node) {
		d.Add(policy.Severity(), "%s: part %d %s part %d (was %q)",
			pc.Path(), part, verb, origin[oc], oc.Text)
	}

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
		// Sections bound to different definitions cannot share children.
		if oc.Def != pc.Def && (ident.IsSection(oc) || ident.IsSection(pc)) {
			conflict(oc, pc)
			if !policy.Strict {
				clone := m.cloneSubtree(pc, part)
				outParent.ReplaceChild(oc, clone)
				byIdent[id] = clone
			}
			continue
		}
		if pc.Text != oc.Text ||
			!slices.Equal(pc.Block, oc.Block) {
			// Union valid list slots; handle other different values as conflicts.
			if unionListSlots(oc, pc) {
				continue
			}
			conflict(oc, pc)
			if !policy.Strict {
				oc.SetValueFrom(pc)
				origin[oc] = part
			}
		}
		// Keep the winning header and merge children from the later section.
		if ident.IsSection(pc) {
			m.level(oc, pc, part)
		}
	}
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

// unionListSlots unions two valid list leaves in the same slot and rewrites the output node canonically.
func unionListSlots(oc, pc *tree.Node) bool {
	def := oc.Def
	if def == nil || def != pc.Def || !slices.Equal(oc.Block, pc.Block) {
		return false
	}
	ls := def.ListSpec
	if ls.Arg == "" {
		return false
	}
	kw := ls.Keywords()
	a, aerr := listval.Resolve(oc.Fields[ls.Arg], ls.Sep, kw)
	b, berr := listval.Resolve(pc.Fields[ls.Arg], ls.Sep, kw)
	if aerr != nil || berr != nil {
		return false
	}
	for arg, v := range oc.Fields {
		if arg != ls.Arg && pc.Fields[arg] != v {
			return false
		}
	}
	// Keep the existing spelling when both values resolve to an empty set without a declared None spelling.
	raw := listval.Canonical(append(a, b...), ls.Sep, kw)
	if raw == "" {
		return true
	}
	f := maps.Clone(oc.Fields)
	f[ls.Arg] = raw
	oc.Def, oc.Fields = def, f
	// Set text last so the parent lookup uses the final value.
	oc.Text = def.Render(f)
	return true
}
