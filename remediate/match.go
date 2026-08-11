package remediate

import (
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/tree"
)

// indexByIdent maps the first node for each identity and warns about later nodes that Diff cannot pair.
func indexByIdent(
	nodes []*tree.Node,
	d *diag.Diagnostics,
) map[ident.Ident]*tree.Node {
	m := make(map[ident.Ident]*tree.Node, len(nodes))
	for _, n := range nodes {
		id := ident.Of(n)
		if prev, ok := m[id]; ok {
			d.AddAt(n.Line, diag.Warning,
				"%s: duplicate of %q ignored by diff (first occurrence wins)",
				n.Path(), prev.Text)
			continue
		}
		m[id] = n
	}
	return m
}
