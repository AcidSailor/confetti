package remediate

import (
	"github.com/acidsailor/confetti/schema"
)

// buildOrderIndex assigns each schema node its first pre-order declaration rank.
func buildOrderIndex(s *schema.Schema) map[*schema.Node]int {
	idx := map[*schema.Node]int{}
	s.Walk(func(n *schema.Node) { idx[n] = len(idx) })
	return idx
}
