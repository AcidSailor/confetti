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
	opensBlock  bool   // the definition declares a block; closesBlock reports whether one stayed open
	closesBlock bool   // the opener also carried its terminator, so the body is empty
	nearClose   bool   // the opener carried a terminator it could not close on, so the block stays open
	term        string // the terminator of an opened block, reported with nearClose
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
	closes, near, term := false, false, ""
	if opens {
		term = def.Block.Term(fields)
		if head, f, ok := inlineClose(top.children, def, txt, term); ok {
			txt, fields, closes = head, f, true
		} else {
			near = carriesTerm(def, txt, term)
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
		term:        term,
	}
}

// inlineClose cuts trailing terminators off a BlockDelim opener while the shorter text still binds the same definition and delimiter, and returns the shortest such text.
func inlineClose(
	candidates []*schema.Def,
	def *schema.Def,
	txt, term string,
) (string, map[string]string, bool) {
	if def.Block.Kind != schema.BlockDelim {
		return "", nil, false
	}
	head, fields, ok := cutTerm(candidates, def, txt, term)
	if !ok {
		return "", nil, false
	}
	// Cut to the first terminator so the node re-parses unchanged from its rendered multi-line form.
	for {
		shorter, f, ok := cutTerm(candidates, def, head, term)
		if !ok {
			return head, fields, true
		}
		head, fields = shorter, f
	}
}

// cutTerm removes one trailing terminator and re-matches the shorter text against all candidates at the level.
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
	d, fields, ok := schema.MatchChild(candidates, head)
	if !ok || d != def || d.Block.Term(fields) != term {
		return "", nil, false
	}
	return head, fields, true
}

// carriesTerm reports whether a BlockDelim opener ends with a terminator that is not just its own delimiter token.
func carriesTerm(def *schema.Def, txt, term string) bool {
	if def.Block.Kind != schema.BlockDelim {
		return false
	}
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
