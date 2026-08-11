package remediate

import (
	"github.com/acidsailor/confetti/schema"
)

// buildOrderIndex assigns each schema node its first pre-order declaration rank.
func buildOrderIndex(s *schema.Schema) map[*schema.Node]int {
	idx := map[*schema.Node]int{}
	i := 0
	var walk func(nodes []*schema.Node)
	walk = func(nodes []*schema.Node) {
		for _, n := range nodes {
			if _, seen := idx[n]; seen {
				continue
			}
			idx[n] = i
			i++
			walk(n.Children)
		}
	}
	walk(s.Roots)
	return idx
}
