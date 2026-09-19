package schema

import (
	"maps"
	"strings"
)

// InlineClose removes trailing terminators while preserving the matched definition and delimiter.
func InlineClose(
	candidates []*Def,
	def *Def,
	txt, term string,
) (string, map[string]string, bool) {
	var fields map[string]string
	closed := false
	// Repeated removal keeps canonical output idempotent.
	for {
		head, f, ok := cutTerm(candidates, def, txt, term)
		if !ok {
			return txt, fields, closed
		}
		txt, fields, closed = head, f, true
	}
}

// cutTerm removes one trailing terminator if the remaining text binds the same definition and delimiter.
func cutTerm(
	candidates []*Def,
	def *Def,
	txt, term string,
) (string, map[string]string, bool) {
	head, ok := strings.CutSuffix(txt, term)
	if !ok {
		return "", nil, false
	}
	head = strings.TrimRight(head, " ")
	// The remaining text must still contain the captured delimiter.
	if !strings.Contains(head, term) {
		return "", nil, false
	}
	fields, ok := BindsDef(candidates, def, head)
	if !ok || def.Block.Term(fields) != term {
		return "", nil, false
	}
	return head, fields, true
}

// InlineBlock returns the one-line form of a BlockDelim node with an empty body.
// It reports false unless parsing that line among the node's siblings closes the
// block inline and restores the same opener and fields.
func (n *Node) InlineBlock() (string, bool) {
	def := n.Def
	if def == nil || def.Block.Kind != BlockDelim || len(n.Block) > 0 {
		return "", false
	}
	candidates := def.Schema.Roots
	if p := n.Parent; p != nil && p.Def != nil {
		candidates = p.Def.Children
	}
	opener := def.Render(n.Fields)
	term := def.Block.Term(n.Fields)
	line := opener + " " + term
	fields, ok := BindsDef(candidates, def, line)
	if !ok || def.Block.Term(fields) != term {
		return "", false
	}
	head, fields, ok := InlineClose(candidates, def, line, term)
	if !ok || head != opener || !maps.Equal(fields, n.Fields) {
		return "", false
	}
	return line, true
}
