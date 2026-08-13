package remediate

import (
	"github.com/acidsailor/confetti/schema"

	"github.com/acidsailor/confetti/internal/ident"
)

// indexByIdent maps the first node for each identity; callers detect a later duplicate by comparing the mapped node.
func indexByIdent(nodes []*schema.Node) map[ident.Ident]*schema.Node {
	m := make(map[ident.Ident]*schema.Node, len(nodes))
	for _, n := range nodes {
		if id := ident.Of(n); m[id] == nil {
			m[id] = n
		}
	}
	return m
}

// groupByIdent preserves every node for identities that appear more than once.
func groupByIdent(nodes []*schema.Node) map[ident.Ident][]*schema.Node {
	m := make(map[ident.Ident][]*schema.Node, len(nodes))
	for _, n := range nodes {
		id := ident.Of(n)
		m[id] = append(m[id], n)
	}
	return m
}

// runningCounterpart selects the stale spelling to replace before slot cleanup.
func runningCounterpart(
	nodes []*schema.Node,
	intended *schema.Node,
) (*schema.Node, bool) {
	first := nodes[0]
	if len(nodes) == 1 || ident.CategoryOf(intended) != ident.KindedSingle {
		return first, false
	}
	for _, n := range nodes {
		if !n.SameValue(intended) {
			return n, true
		}
	}
	return first, false
}
