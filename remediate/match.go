package remediate

import (
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/tree"
)

// indexByIdent maps the first node for each identity; callers detect a later duplicate by comparing the mapped node.
func indexByIdent(nodes []*tree.Node) map[ident.Ident]*tree.Node {
	m := make(map[ident.Ident]*tree.Node, len(nodes))
	for _, n := range nodes {
		if id := ident.Of(n); m[id] == nil {
			m[id] = n
		}
	}
	return m
}
