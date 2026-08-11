package compare

import (
	"strings"

	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/lcp"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

const (
	indentUnit   = "  " // Match render's canonical two-space indentation.
	indentAdd    = "+ "
	indentRemove = "- "
)

// Render groups changes by section path in first-seen order and emits shared ancestor context once.
func Render(changes []remediate.Change) string {
	keys := make([]string, 0, len(changes))
	groups := map[string][]remediate.Change{}
	paths := map[string][]string{}
	for _, c := range changes {
		k := strings.Join(c.Path, "\x00")
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
			paths[k] = c.Path
		}
		groups[k] = append(groups[k], c)
	}
	var b strings.Builder
	var stack []string
	for _, k := range keys {
		path := paths[k]
		stack = stack[:lcp.Len(stack, path)]
		for _, s := range path[len(stack):] {
			writeLine(&b, indentUnit, len(stack), s)
			stack = append(stack, s)
		}
		for _, c := range groups[k] {
			renderChange(&b, c, len(stack))
		}
	}
	return b.String()
}

// renderChange writes one signed logical change and limits section modifications to their headers.
func renderChange(b *strings.Builder, c remediate.Change, depth int) {
	if c.Action == graph.Modify && c.Running != nil && c.Intended != nil &&
		(len(c.Running.Children) > 0 || len(c.Intended.Children) > 0) {
		writeLine(b, indentRemove, depth, lineText(c.Running))
		writeLine(b, indentAdd, depth, lineText(c.Intended))
		return
	}
	if c.Running != nil {
		writeTree(b, indentRemove, c.Running, depth)
	}
	if c.Intended != nil {
		writeTree(b, indentAdd, c.Intended, depth)
	}
}

// writeTree writes a signed node and its complete subtree.
func writeTree(b *strings.Builder, sign string, n *tree.Node, depth int) {
	writeLine(b, sign, depth, lineText(n))
	if def := n.Def; def != nil && def.Block.Kind != schema.BlockNone {
		// Preserve the block body at column zero after the sign.
		for _, l := range n.Block {
			b.WriteString(sign)
			b.WriteString(l)
			b.WriteByte('\n')
		}
		writeLine(b, sign, 0, def.Block.Term(n.Fields))
		return // block nodes have no children
	}
	for _, c := range n.Children {
		writeTree(b, sign, c, depth+1)
	}
}

// lineText renders a matched node canonically and returns raw text for a node without a definition.
func lineText(n *tree.Node) string {
	if def := n.Def; def != nil {
		return def.Render(n.Fields)
	}
	return n.Text
}

func writeLine(b *strings.Builder, sign string, depth int, text string) {
	b.WriteString(sign)
	b.WriteString(strings.Repeat(indentUnit, depth))
	b.WriteString(text)
	b.WriteByte('\n')
}
