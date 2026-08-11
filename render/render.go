package render

import (
	"strings"

	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

const indentUnit = "  " // Use canonical two-space indentation.

// Render emits canonical text in tree order.
func Render(cfg *tree.Config) string {
	var b strings.Builder
	for _, n := range cfg.Root.Children {
		renderNode(&b, n, 0)
	}
	return b.String()
}

func renderNode(b *strings.Builder, n *tree.Node, depth int) {
	def := n.Def
	indent := strings.Repeat(indentUnit, depth)
	b.WriteString(indent)
	if def != nil {
		b.WriteString(def.Render(n.Fields))
	} else {
		b.WriteString(n.Text)
	}
	b.WriteByte('\n')
	// Preserve raw block bodies at column zero and append the terminator.
	if def != nil && def.Block.Kind != schema.BlockNone {
		for _, l := range n.Block {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		b.WriteString(def.Block.Term(n.Fields))
		b.WriteByte('\n')
		return // Block nodes have no children or section-exit tokens.
	}
	for _, c := range n.Children {
		renderNode(b, c, depth+1)
	}
	// Emit a declared section-exit token at the section's indentation.
	if def != nil && def.SectionExitToken != "" {
		b.WriteString(indent)
		b.WriteString(def.SectionExitToken)
		b.WriteByte('\n')
	}
}
