package schema

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"

	"github.com/acidsailor/confetti/value"
)

type tokenKind int

const (
	litToken tokenKind = iota
	capToken
)

type token struct {
	kind tokenKind
	text string // literal text, or capture name for capToken
	typ  string // value-type name (capToken only)
}

// matchSpec compiles a "{{ name:type }}" template for parsing and rendering.
type matchSpec struct {
	tokens      []token
	re          *regexp.Regexp
	prefixRe    *regexp.Regexp // re without the end anchor, for block openers
	argTypes    map[string]string
	emptyArgs   map[string]bool // capture args whose type pattern matches ""
	oneCharArgs map[string]bool // capture args whose type pattern matches one rune
	litLen      int             // total literal length, precomputed for specificity ordering
}

func compileSpec(tmpl string, reg *value.Registry) (*matchSpec, error) {
	toks, err := parseTemplate(tmpl)
	if err != nil {
		return nil, err
	}
	m := &matchSpec{
		tokens:      toks,
		argTypes:    map[string]string{},
		emptyArgs:   map[string]bool{},
		oneCharArgs: map[string]bool{},
	}
	seen := make(map[string]bool)
	var b strings.Builder
	b.WriteString("^")
	for i, t := range toks {
		if t.kind == litToken {
			b.WriteString(regexp.QuoteMeta(t.text))
			m.litLen += len(t.text)
			continue
		}
		if seen[t.text] {
			return nil, fmt.Errorf(
				"duplicate capture name %q in %q",
				t.text,
				tmpl,
			)
		}
		seen[t.text] = true
		vt, ok := reg.Get(t.typ)
		if !ok {
			return nil, fmt.Errorf("unknown value type %q in %q", t.typ, tmpl)
		}
		pat := vt.Pattern
		// Registry.Register already compiled the pattern; failure here only prevents the empty-match flag.
		if anchored, err := regexp.Compile("^(?:" + pat + ")$"); err == nil {
			if anchored.MatchString("") {
				m.emptyArgs[t.text] = true
			}
			// A delimiter must be exactly one non-space character.
			if maxRunes(pat) == 1 && !anchored.MatchString(" ") {
				m.oneCharArgs[t.text] = true
			}
		}
		if i != len(toks)-1 {
			// Make non-terminal captures lazy so a following literal remains matchable.
			pat = lazify(pat)
		}
		b.WriteString("(?P<")
		b.WriteString(t.text)
		b.WriteString(">")
		b.WriteString(pat)
		b.WriteString(")")
		m.argTypes[t.text] = t.typ
	}
	// A block opener matches a prefix: its delimiter is followed by body text.
	prefixRe, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("compiling %q: %w", tmpl, err)
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("compiling %q: %w", tmpl, err)
	}
	m.re, m.prefixRe = re, prefixRe
	return m, nil
}

// maxRunes returns the longest match of pattern in runes, or -1 when unbounded
// or unparsable. It lets a schema require a capture of exactly one character.
func maxRunes(pattern string) int {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return -1
	}
	return maxRunesOf(re.Simplify())
}

func maxRunesOf(re *syntax.Regexp) int {
	switch re.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine,
		syntax.OpBeginText, syntax.OpEndText, syntax.OpWordBoundary,
		syntax.OpNoWordBoundary:
		return 0
	case syntax.OpLiteral:
		return len(re.Rune)
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return 1
	case syntax.OpCapture:
		return maxRunesOf(re.Sub[0])
	case syntax.OpQuest:
		return maxRunesOf(re.Sub[0])
	case syntax.OpConcat:
		total := 0
		for _, sub := range re.Sub {
			n := maxRunesOf(sub)
			if n < 0 {
				return -1
			}
			total += n
		}
		return total
	case syntax.OpAlternate:
		best := 0
		for _, sub := range re.Sub {
			n := maxRunesOf(sub)
			if n < 0 {
				return -1
			}
			best = max(best, n)
		}
		return best
	case syntax.OpRepeat:
		n := maxRunesOf(re.Sub[0])
		if n < 0 || re.Max < 0 {
			return -1
		}
		return n * re.Max
	default: // OpStar, OpPlus, and anything unrecognized are unbounded.
		return -1
	}
}

// lazify makes an unescaped trailing plus or asterisk lazy and leaves all other patterns unchanged.
func lazify(pattern string) string {
	last := len(pattern) - 1
	if last < 0 || (pattern[last] != '+' && pattern[last] != '*') {
		return pattern
	}
	// An odd number of preceding backslashes escapes the quantifier.
	bs := last - len(strings.TrimRight(pattern[:last], `\`))
	if bs%2 == 1 {
		return pattern
	}
	return pattern + "?"
}

func parseTemplate(tmpl string) ([]token, error) {
	var toks []token
	for tmpl != "" {
		i := strings.Index(tmpl, "{{")
		if i < 0 {
			toks = append(toks, token{kind: litToken, text: tmpl})
			break
		}
		if i > 0 {
			toks = append(toks, token{kind: litToken, text: tmpl[:i]})
		}
		jRel := strings.Index(tmpl[i+2:], "}}")
		if jRel < 0 {
			return nil, fmt.Errorf("unterminated {{ in %q", tmpl)
		}
		j := i + 2 + jRel
		inner := strings.TrimSpace(tmpl[i+2 : j])
		name, typ := inner, "word"
		if before, after, ok := strings.Cut(inner, ":"); ok {
			name = strings.TrimSpace(before)
			typ = strings.TrimSpace(after)
		}
		if name == "" {
			return nil, fmt.Errorf("empty capture name in %q", tmpl)
		}
		toks = append(toks, token{kind: capToken, text: name, typ: typ})
		tmpl = tmpl[j+2:]
	}
	return toks, nil
}

// Match returns captured fields and true if line matches this spec exactly.
func (m *matchSpec) Match(line string) (map[string]string, bool) {
	sm := m.re.FindStringSubmatch(line)
	if sm == nil {
		return nil, false
	}
	fields := make(map[string]string, len(m.argTypes))
	for name := range m.argTypes {
		fields[name] = sm[m.re.SubexpIndex(name)]
	}
	return fields, true
}

// MatchPrefix returns captured fields and the offset after the match, or false.
func (m *matchSpec) MatchPrefix(line string) (map[string]string, int, bool) {
	loc := m.prefixRe.FindStringSubmatchIndex(line)
	if loc == nil {
		return nil, 0, false
	}
	fields := make(map[string]string, len(m.argTypes))
	for name := range m.argTypes {
		i := m.prefixRe.SubexpIndex(name) * 2
		if loc[i] < 0 {
			continue
		}
		fields[name] = line[loc[i]:loc[i+1]]
	}
	return fields, loc[1], true
}

// Render produces the line by interleaving literals with field values.
func (m *matchSpec) Render(fields map[string]string) string {
	var b strings.Builder
	for _, t := range m.tokens {
		if t.kind == litToken {
			b.WriteString(t.text)
		} else {
			b.WriteString(fields[t.text])
		}
	}
	return b.String()
}

// ArgType returns the value-type name declared for a capture arg.
func (m *matchSpec) ArgType(name string) string { return m.argTypes[name] }
