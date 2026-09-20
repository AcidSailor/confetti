package render

import (
	"strings"

	"github.com/acidsailor/confetti/schema"
)

const indentUnit = "  "

// Render emits canonical text in tree order.
func Render(cfg *schema.Config) string {
	var b strings.Builder
	for _, n := range cfg.Root.Children {
		renderNode(&b, n, 0)
	}
	return b.String()
}

func renderNode(b *strings.Builder, n *schema.Node, depth int) {
	def := n.Def
	if def != nil && def.Block.Kind == schema.BlockDelim {
		// schema.DelimLines carries the whole shape: nil for an empty body,
		// which a device omits from its running configuration so emitting one
		// would invent a command (validate.BlockBodies reports it, because
		// Render has no diagnostic channel), otherwise the opener followed by
		// the body lines at column zero.
		lines := schema.DelimLines(n)
		if lines == nil {
			return
		}
		b.WriteString(strings.Repeat(indentUnit, depth))
		for _, l := range lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		return // setBlock forbids children and a section-exit token on a block.
	}
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
		return // setBlock forbids children and a section-exit token on a block.
	}
	for _, c := range n.Children {
		renderNode(b, c, depth+1)
	}
	if def != nil && def.SectionExitToken != "" {
		b.WriteString(indent)
		b.WriteString(def.SectionExitToken)
		b.WriteByte('\n')
	}
}
