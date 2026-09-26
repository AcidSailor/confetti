package validate

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// BlockBodies reports delimited blocks that cannot be rendered faithfully.
//
// Empty bodies and bodies containing their delimiter cannot survive a round
// trip. Parse prevents both, but callers, transforms, and merge resolvers can
// construct them. This check provides the diagnostics that renderers cannot.
func BlockBodies(cfg *schema.Config, d *diag.Diagnostics) {
	for n := range cfg.All() {
		def := n.Def
		if def == nil || def.Block.Kind != schema.BlockDelim {
			continue
		}
		if n.EmptyDelimBody() {
			d.AddAt(
				n.Line,
				diag.Error,
				"%s: delimited block has no body; a device omits the empty form, so this node renders to nothing",
				n.Path(),
			)
			continue
		}
		// A delimiter in the body would close the block early. It is one
		// non-space rune, so each body line can be checked separately.
		term := def.Block.Term(n.Fields)
		if slices.ContainsFunc(n.Block, func(l string) bool {
			return strings.Contains(l, term)
		}) {
			d.AddAt(
				n.Line,
				diag.Error,
				"%s: delimited block body contains its own delimiter %q, so it would close early",
				n.Path(),
				term,
			)
		}
	}
}
