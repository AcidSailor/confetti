package parse

import (
	"strings"

	"github.com/acidsailor/confetti/schema"
)

// frame represents one section; a frame without schema children isolates descendants of an unknown line.
type frame struct {
	indent   int
	children []*schema.Def
}

type stepKind int

const (
	stepBlank    stepKind = iota // whitespace-only line
	stepBody                     // inside an open raw block, not the terminator
	stepBlockEnd                 // the terminator line of an open raw block
	stepUnknown                  // no schema candidate matched
	stepMatched                  // a schema candidate matched
)

// step reports how the scanner classified one input line.
type step struct {
	kind        stepKind
	lineNo      int // 1-based
	txt         string
	indent      int
	depth       int // stack depth after this line, including the root frame
	def         *schema.Def
	fields      map[string]string
	opensBlock  bool // the definition declares a block
	closesBlock bool // the block closes on this line with an empty body
	nearClose   bool // an invalid inline close leaves the block open
}

// scanner drives the indent-stack walk that Parse and BlockSpans must perform identically.
type scanner struct {
	stack  []frame
	term   string // A non-empty value identifies an open raw block.
	lineNo int
}

func newScanner(s *schema.Schema) *scanner {
	return &scanner{stack: []frame{{indent: -1, children: s.Roots}}}
}

// inBlock reports whether the last scanned line left a raw block open.
func (sc *scanner) inBlock() bool { return sc.term != "" }

// line classifies one raw input line and updates the section stack.
func (sc *scanner) line(raw string) step {
	sc.lineNo++
	if sc.term != "" {
		// Terminator comparison ignores trailing whitespace only.
		if strings.TrimRight(raw, " \t\r") == sc.term {
			sc.term = ""
			return step{kind: stepBlockEnd, lineNo: sc.lineNo}
		}
		return step{kind: stepBody, lineNo: sc.lineNo}
	}
	indent := countIndent(raw)
	txt := normalize(raw)
	if txt == "" {
		return step{kind: stepBlank, lineNo: sc.lineNo}
	}
	// Pop sections at or below this indentation level.
	for len(sc.stack) > 1 && indent <= sc.stack[len(sc.stack)-1].indent {
		sc.stack = sc.stack[:len(sc.stack)-1]
	}
	top := &sc.stack[len(sc.stack)-1]
	def, fields, ok := schema.MatchChild(top.children, txt)
	if !ok {
		// Isolate deeper lines under this unknown block.
		sc.stack = append(sc.stack, frame{indent: indent})
		return step{
			kind:   stepUnknown,
			lineNo: sc.lineNo,
			txt:    txt,
			indent: indent,
			depth:  len(sc.stack),
		}
	}
	opens := def.Block.Kind != schema.BlockNone
	closes, near := false, false
	if opens {
		term := def.Block.Term(fields)
		if def.Block.Kind == schema.BlockDelim {
			if head, f, ok := inlineClose(top.children, def, txt, term); ok {
				txt, fields, closes = head, f, true
			} else {
				near = carriesTerm(txt, term)
			}
		}
		if !closes {
			sc.term = term
		}
	}
	sc.stack = append(sc.stack, frame{indent: indent, children: def.Children})
	return step{
		kind:        stepMatched,
		lineNo:      sc.lineNo,
		txt:         txt,
		indent:      indent,
		depth:       len(sc.stack),
		def:         def,
		fields:      fields,
		opensBlock:  opens,
		closesBlock: closes,
		nearClose:   near,
	}
}

// inlineClose removes trailing terminators while preserving the matched definition and delimiter.
func inlineClose(
	candidates []*schema.Def,
	def *schema.Def,
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
	candidates []*schema.Def,
	def *schema.Def,
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
	fields, ok := schema.BindsDef(candidates, def, head)
	if !ok || def.Block.Term(fields) != term {
		return "", nil, false
	}
	return head, fields, true
}

// carriesTerm reports whether a trailing terminator has another occurrence before it.
func carriesTerm(txt, term string) bool {
	head, ok := strings.CutSuffix(txt, term)
	return ok && strings.Contains(head, term)
}

func countIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 8
		default:
			return n
		}
	}
	return n
}

// normalize trims and collapses internal whitespace to single spaces.
func normalize(line string) string {
	return strings.Join(strings.Fields(line), " ")
}
