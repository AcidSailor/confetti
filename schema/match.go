package schema

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"

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
	tokens    []token
	re        *regexp.Regexp
	prefixRe  *regexp.Regexp // re without the end anchor, for block openers
	argTypes  map[string]string
	argGroups map[string]int  // capture name to subexpression index, shared by both regexps
	emptyArgs map[string]bool // capture args whose type pattern matches ""
	litLen    int             // total literal length, precomputed for specificity ordering
}

func compileSpec(tmpl string, reg *value.Registry) (*matchSpec, error) {
	toks, err := parseTemplate(tmpl)
	if err != nil {
		return nil, err
	}
	m := &matchSpec{
		tokens:    toks,
		argTypes:  map[string]string{},
		argGroups: map[string]int{},
		emptyArgs: map[string]bool{},
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
		// Registry.Register already compiled the pattern; failure here only
		// prevents the empty-match flag, which makes mustNonEmptyArg permissive.
		if anchored, err := regexp.Compile("^(?:" + pat + ")$"); err == nil &&
			anchored.MatchString("") {
			m.emptyArgs[t.text] = true
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
	// Both regexps use the same capture indices; resolve names once.
	for name := range m.argTypes {
		m.argGroups[name] = re.SubexpIndex(name)
	}
	return m, nil
}

// oneNonSpaceRune checks BlockDelim's capture constraint on demand. maxRunes
// supplies the upper bound; the empty-match check supplies the lower bound.
// Whitespace and invalid patterns are rejected.
func (m *matchSpec) oneNonSpaceRune(arg string, reg *value.Registry) bool {
	vt, ok := reg.Get(m.argTypes[arg])
	if !ok {
		return false
	}
	anchored, err := regexp.Compile("^(?:" + vt.Pattern + ")$")
	if err != nil {
		return false
	}
	return maxRunes(vt.Pattern) == 1 && !anchored.MatchString("") &&
		!matchesSpace(anchored)
}

// matchesSpace reports whether an anchored pattern can match a single
// whitespace rune. These runes cannot serve as delimiters because
// NormalizeLine trims or collapses them before matching.
func matchesSpace(anchored *regexp.Regexp) bool {
	for _, c := range spaceRunes {
		if anchored.MatchString(string(c)) {
			return true
		}
	}
	return false
}

// spaceRunes lists the Unicode whitespace recognized by NormalizeLine.
var spaceRunes = func() []rune {
	var out []rune
	for _, r := range unicode.White_Space.R16 {
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			out = append(out, c)
		}
	}
	for _, r := range unicode.White_Space.R32 {
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			out = append(out, c)
		}
	}
	return out
}()

// maxRunes returns an upper bound on the pattern's match length in runes.
// It returns -1 for invalid, unbounded, or unsupported patterns so callers
// requiring a bound reject them.
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
	default: // OpStar, OpPlus, and anything unrecognized: refuse to bound it.
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
	fields, _, ok := m.matchWith(m.re, line)
	return fields, ok
}

// MatchPrefix matches from the start of line and returns the captured fields
// and the byte offset where the match ends. Like Match, it includes every arg.
func (m *matchSpec) MatchPrefix(line string) (map[string]string, int, bool) {
	return m.matchWith(m.prefixRe, line)
}

// matchWith binds every capture against re and reports where the match ends.
func (m *matchSpec) matchWith(
	re *regexp.Regexp,
	line string,
) (map[string]string, int, bool) {
	loc := re.FindStringSubmatchIndex(line)
	if loc == nil {
		return nil, 0, false
	}
	fields := make(map[string]string, len(m.argGroups))
	for name, g := range m.argGroups {
		// A group that did not participate reports -1; report it as empty so
		// every arg is present either way.
		if loc[2*g] < 0 {
			fields[name] = ""
			continue
		}
		fields[name] = line[loc[2*g]:loc[2*g+1]]
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
