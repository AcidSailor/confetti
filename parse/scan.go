package parse

import (
	"strings"
	"unicode"
	"unicode/utf8"

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
	stepBlockEnd                 // the terminator of an open raw block appears on this line
	stepUnknown                  // no schema candidate matched
	stepMatched                  // a schema candidate matched
)

// step reports how the scanner classified one input line.
type step struct {
	kind       stepKind
	lineNo     int // 1-based
	txt        string
	indent     int
	depth      int // stack depth after this line, including the root frame
	def        *schema.Def
	fields     map[string]string
	opensBlock bool   // the definition declares a block
	body       string // raw block text this line contributes, before any terminator
	closed     bool   // a BlockDelim closes on this line; body may be empty
	tail       string // text after the terminator, which cannot be part of the block
}

// scanner drives the indent-stack walk that Parse and BlockSpans must perform identically.
type scanner struct {
	stack  []frame
	term   string // A non-empty value identifies an open raw block.
	delim  bool   // The open block ends at the next delimiter, not at a whole line.
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
		return sc.bodyLine(raw)
	}
	indent := countIndent(raw)
	txt := schema.NormalizeLine(raw)
	if txt == "" {
		return step{kind: stepBlank, lineNo: sc.lineNo}
	}
	// Pop sections at or below this indentation level.
	for len(sc.stack) > 1 && indent <= sc.stack[len(sc.stack)-1].indent {
		sc.stack = sc.stack[:len(sc.stack)-1]
	}
	top := &sc.stack[len(sc.stack)-1]
	def, fields, end, ok := schema.MatchChildOpener(top.children, txt)
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
	st := step{
		kind:       stepMatched,
		lineNo:     sc.lineNo,
		txt:        txt[:end],
		indent:     indent,
		def:        def,
		fields:     fields,
		opensBlock: def.Block.Kind != schema.BlockNone,
	}
	if def.Block.Kind == schema.BlockDelim {
		// Body text keeps its original spacing, so measure it against the raw line.
		rest := raw[rawOffset(raw, txt, end):]
		st.body, st.closed, st.tail = splitAtTerm(rest, def.Block.Term(fields))
		if !st.closed {
			sc.term, sc.delim = def.Block.Term(fields), true
		}
	} else if st.opensBlock {
		sc.term, sc.delim = def.Block.Term(fields), false
	}
	sc.stack = append(sc.stack, frame{indent: indent, children: def.Children})
	st.depth = len(sc.stack)
	return st
}

// bodyLine classifies a line inside an open raw block.
func (sc *scanner) bodyLine(raw string) step {
	st := step{lineNo: sc.lineNo, kind: stepBody, body: raw}
	if !sc.delim {
		// A literal terminator owns its line; only trailing whitespace is ignored.
		if strings.TrimRight(raw, " \t\r") == sc.term {
			sc.term = ""
			return step{kind: stepBlockEnd, lineNo: sc.lineNo}
		}
		return st
	}
	body, closed, tail := splitAtTerm(raw, sc.term)
	if !closed {
		return st
	}
	sc.term = ""
	return step{
		kind:   stepBlockEnd,
		lineNo: sc.lineNo,
		body:   body,
		closed: true,
		tail:   tail,
	}
}

// splitAtTerm cuts text at the first terminator, reporting the body before it and the text after.
func splitAtTerm(text, term string) (body string, closed bool, tail string) {
	if term == "" {
		// BlockDelim forbids empty delimiters; strings.Cut would close immediately.
		panic("parse: empty block terminator (schema.BlockDelim rejects one)")
	}
	before, after, ok := strings.Cut(text, term)
	if !ok {
		return text, false, ""
	}
	return before, true, after
}

// rawOffset maps a normalized byte offset to the raw line. It must use the
// same Unicode whitespace rules as NormalizeLine to locate the body correctly.
func rawOffset(raw, norm string, end int) int {
	i, n := skipSpace(raw, 0), 0
	for n < end && i < len(raw) {
		if norm[n] == ' ' {
			i = skipSpace(raw, i)
			n++
			continue
		}
		_, w := utf8.DecodeRuneInString(raw[i:])
		i += w
		n += w
	}
	if n < end {
		// norm is NormalizeLine(raw); a partial offset would misplace the body.
		panic("parse: rawOffset desynchronized from schema.NormalizeLine")
	}
	return i
}

// skipSpace returns the offset of the first non-space rune at or after i.
func skipSpace(s string, i int) int {
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += w
	}
	return i
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
