package validate

import (
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// BlockBodies reports delimited blocks that cannot be rendered faithfully.
//
// Parse cannot produce either shape, so a node that carries one was assembled by
// a caller, a tree transform, or a merge resolver. Both render to text that does
// not read back as the node it came from, and render reports nothing, so without
// this check the defect surfaces only as remediation that never converges.
func BlockBodies(cfg *schema.Config, d *diag.Diagnostics) {
	schema.Walk(cfg, func(n *schema.Node) {
		def := n.Def
		if def == nil || def.Block.Kind != schema.BlockDelim {
			return
		}
		body := strings.Join(n.Block, "\n")
		if body == "" {
			d.AddAt(
				n.Line,
				diag.Error,
				"%s: delimited block has no body; a device omits the empty form, so this node renders to nothing",
				n.Path(),
			)
			return
		}
		// A device closes the block at the delimiter's next occurrence, so a body
		// carrying the delimiter renders as a shorter block plus stray text.
		if term := def.Block.Term(n.Fields); strings.Contains(body, term) {
			d.AddAt(
				n.Line,
				diag.Error,
				"%s: delimited block body contains its own delimiter %q, so it would close early",
				n.Path(),
				term,
			)
		}
	})
}
