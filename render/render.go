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
	// A device omits an empty delimited block, so emitting one would invent a
	// command that cannot survive a round trip through the device. A nil Block
	// on a BlockDelim node is dropped the same way; validate.BlockBodies is what
	// reports it, because Render has no diagnostic channel.
	if def != nil && def.Block.Kind == schema.BlockDelim &&
		strings.Join(n.Block, "\n") == "" {
		return
	}
	indent := strings.Repeat(indentUnit, depth)
	b.WriteString(indent)
	if def != nil {
		b.WriteString(def.Render(n.Fields))
	} else {
		b.WriteString(n.Text)
	}
	// The body runs from the opener's delimiter to the next one, so joining it
	// reproduces the body byte-exact between the two delimiters, on one line or
	// many. The opener itself is re-rendered canonically.
	if def != nil && def.Block.Kind == schema.BlockDelim {
		b.WriteString(strings.Join(n.Block, "\n"))
		b.WriteString(def.Block.Term(n.Fields))
		b.WriteByte('\n')
		return // setBlock forbids children and a section-exit token on a block.
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
