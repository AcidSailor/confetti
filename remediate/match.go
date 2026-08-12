package remediate

import (
	"slices"

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

// groupByIdent preserves every node for identities that appear more than once.
func groupByIdent(nodes []*tree.Node) map[ident.Ident][]*tree.Node {
	m := make(map[ident.Ident][]*tree.Node, len(nodes))
	for _, n := range nodes {
		id := ident.Of(n)
		m[id] = append(m[id], n)
	}
	return m
}

// sameValue reports whether two nodes carry the same definition, rendered line, and block body.
func sameValue(a, b *tree.Node) bool {
	return a.Def == b.Def && a.Text == b.Text && slices.Equal(a.Block, b.Block)
}

// runningCounterpart selects the stale spelling to replace before slot cleanup.
func runningCounterpart(
	nodes []*tree.Node,
	intended *tree.Node,
) (*tree.Node, bool) {
	first := nodes[0]
	if len(nodes) == 1 || ident.CategoryOf(intended) != ident.KindedSingle {
		return first, false
	}
	for _, n := range nodes {
		if !sameValue(n, intended) {
			return n, true
		}
	}
	return first, false
}
