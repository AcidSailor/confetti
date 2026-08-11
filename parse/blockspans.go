package parse

import (
	"strings"

	"github.com/acidsailor/confetti/schema"
)

// BlockSpans marks lines from each recognized raw-block opener through its terminator or EOF and returns nil when the schema has no blocks.
func BlockSpans(s *schema.Schema, text string) []bool {
	if !hasBlocks(s.Roots) {
		return nil
	}
	sc := newScanner(s)
	var spans []bool
	for line := range strings.SplitSeq(text, "\n") {
		st := sc.line(line)
		spans = append(
			spans,
			(st.kind == stepBody) || (st.kind == stepBlockEnd) || st.opensBlock,
		)
	}
	return spans
}

func hasBlocks(nodes []*schema.Node) bool {
	for _, n := range nodes {
		if n.Block.Kind != schema.BlockNone || hasBlocks(n.Children) {
			return true
		}
	}
	return false
}
