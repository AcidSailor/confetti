package schema

import (
	"maps"
	"strings"
)

// InlineClose repeatedly removes a trailing terminator while the remaining text
// still binds def among candidates with the same delimiter. It returns the
// remaining text, its captured fields, and whether anything was removed; on
// false the text is unchanged and fields is nil.
//
// candidates must hold every definition at the level that owns def, because the
// match runs through MatchChild. def must declare a BlockDelim block and term
// must be its terminator for the full line: an empty terminator would strip
// nothing and never converge.
func InlineClose(
	candidates []*Def,
	def *Def,
	txt, term string,
) (string, map[string]string, bool) {
	if def.Block.Kind != BlockDelim || term == "" {
		panic("schema: InlineClose requires a BlockDelim definition " +
			"and a non-empty terminator: " + def.Template)
	}
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
// It reports false unless that line, matched against every definition candidate
// at the node's level, binds this definition and closes inline to the same
// opener and fields.
//
// The candidates come from the parent's definition, so a node must sit in the
// tree it will be rendered from. A node whose parent carries no definition is
// checked against the schema roots, which makes a detached or orphaned nested
// node fall back to the multi-line form rather than emit a line that would bind
// a different definition.
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
	if term == "" {
		return "", false
	}
	line := opener + " " + term
	// Parsing normalizes first, so a line it would rewrite cannot round-trip.
	if NormalizeLine(line) != line {
		return "", false
	}
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
