package remediate

import (
	"github.com/acidsailor/confetti/schema"
)

// buildOrderIndex assigns each schema node its first pre-order declaration rank.
func buildOrderIndex(s *schema.Schema) map[*schema.Def]int {
	idx := map[*schema.Def]int{}
	s.Walk(func(n *schema.Def) { idx[n] = len(idx) })
	return idx
}
