package transform

import (
	"regexp"
	"strings"

	"github.com/acidsailor/confetti/schema"
)

// TextRule transforms one line of raw configuration text before parsing or after rendering.
type TextRule interface {
	Apply(line string) string
}

// TreeTransform mutates a parsed tree.
type TreeTransform interface {
	Apply(cfg *schema.Config)
}

type perLineSub struct {
	re   *regexp.Regexp
	repl string
}

// PerLineSub applies a regex substitution to each line independently.
func PerLineSub(pattern, repl string) (TextRule, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return perLineSub{re: re, repl: repl}, nil
}

func (r perLineSub) Apply(line string) string {
	return r.re.ReplaceAllString(line, r.repl)
}

type dropLines struct{ re *regexp.Regexp }

// DropLines blanks matching lines to preserve source line numbers; export rules retain the blank lines.
func DropLines(pattern string) (TextRule, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return dropLines{re: re}, nil
}

func (r dropLines) Apply(line string) string {
	if r.re.MatchString(line) {
		return ""
	}
	return line
}

// ApplyText runs rules left-to-right over each line, preserving the line count.
func ApplyText(rules []TextRule, text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		for _, r := range rules {
			line = r.Apply(line)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// ApplyTree runs tree transforms left-to-right.
func ApplyTree(transforms []TreeTransform, cfg *schema.Config) {
	for _, t := range transforms {
		t.Apply(cfg)
	}
}
