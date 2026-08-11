package schema

import (
	"fmt"
	"regexp"
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
	tokens    []token
	re        *regexp.Regexp
	argTypes  map[string]string
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
		// Registry.Register already compiled the pattern; failure here only prevents the empty-match flag.
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
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("compiling %q: %w", tmpl, err)
	}
	m.re = re
	return m, nil
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
