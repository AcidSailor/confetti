package remediate

import (
	"maps"
	"slices"

	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// listOrModify returns a list delta for a valid changed list, no operation for equal sets, or a whole-line modification as fallback.
func listOrModify(ic, rc *tree.Node) (createIntent, bool) {
	ci := createIntent{src: ic, run: rc, kind: ckModify}
	def := ic.Def
	// Use a whole-line modification unless both nodes form a valid list pair without a block change.
	if (def == nil) || (def.ListSpec.Arg == "") || (def != rc.Def) ||
		!slices.Equal(ic.Block, rc.Block) {
		return ci, true
	}
	ls := def.ListSpec
	kw := ls.Keywords()
	intItems, ierr := listval.Resolve(ic.Fields[ls.Arg], ls.Sep, kw)
	runItems, rerr := listval.Resolve(rc.Fields[ls.Arg], ls.Sep, kw)
	if ierr != nil || rerr != nil {
		return ci, true // Use a whole-line modification for malformed lists.
	}
	for arg, v := range ic.Fields {
		if arg != ls.Arg && rc.Fields[arg] != v {
			return ci, true // Delta lines cannot express changes to other arguments.
		}
	}
	added := diffItems(intItems, runItems)
	removed := diffItems(runItems, intItems)
	if len(added) == 0 && len(removed) == 0 {
		return ci, false // Equal sets need no operation.
	}
	if ls.AddTmpl == "" {
		return ci, true // Use a whole-line modification when no delta forms exist.
	}
	ci.kind = ckListDelta
	ci.addItems, ci.removeItems = added, removed
	return ci, true
}

// diffItems returns items in a that are absent from b and preserves the order of a.
func diffItems(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	return out
}

// deltaLeaf creates a definition-free OpModify leaf with an explicit compressed ListDelta subset.
func deltaLeaf(
	tmpl string,
	fields map[string]string,
	ls schema.ListStrategy,
	items []string,
) *tree.Node {
	f := maps.Clone(fields)
	f[ls.Arg] = listval.Compress(items, ls.Sep)
	n := tree.NewNode(schema.Interpolate(tmpl, f))
	n.Op = tree.OpModify
	return n
}
