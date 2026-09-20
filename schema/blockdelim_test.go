package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/value"
)

// delimType registers pat as a value type and returns a def that uses it as the
// trailing capture, so a caller can assert only whether BlockDelim accepts it.
func delimType(t *testing.T, pat string) func() {
	t.Helper()
	s := New()
	require.NoError(t, s.Registry.Register(value.Type{Name: "d", Pattern: pat}))
	return func() { s.Node("banner motd {{ d:d }}").BlockDelim("d") }
}

// builtinDelimPattern returns the pattern the built-in "delim" type carries.
func builtinDelimPattern(t *testing.T) string {
	t.Helper()
	vt, ok := value.NewRegistry().Get("delim")
	require.True(t, ok, "the delim value type must be a builtin")
	return vt.Pattern
}

// The delimiter bound is derived from regexp/syntax rather than sampled, so it
// has to hold across the construct space, not just for the built-in pattern.
func TestBlockDelimAcceptsOneNonSpaceRune(t *testing.T) {
	for _, pat := range []string{
		`[!@#^]`, `[a-z]`, `\d`, `\pL`, `\p{Greek}`, `(?:a|b)`,
		`[[:punct:]]`, `x{1,1}`, `(?i)q`, `é`, `\x41`, `[\x{1F600}]`,
		// Read the built-in live rather than copying it, so this case keeps
		// certifying the pattern BlockDelim callers actually get.
		builtinDelimPattern(t),
	} {
		t.Run(pat, func(t *testing.T) {
			assert.NotPanics(t, delimType(t, pat))
		})
	}
}

func TestBlockDelimRejectsOtherPatterns(t *testing.T) {
	cases := map[string]string{
		"unbounded plus":    `\S+`,
		"unbounded star":    `x*`,
		"two runes":         `x{2}`,
		"ranged repeat":     `x{1,3}`,
		"longer alternate":  `a|bc`,
		"alternate lengths": `\S|\S\S`,
		"optional is empty": `\S?`,
		"any matches space": `.`,
		"ascii space":       `[ ^]`,
		"tab":               `[\t^]`,
		"unicode next line": `[\x{0085}^]`,
		"no-break space":    `[\x{00a0}^]`,
		"ideographic space": `[\x{3000}^]`,
		"line separator":    `[\x{2028}^]`,
		// Go's \s and \S are ASCII-only, so \S admits every rune above that
		// NormalizeLine folds away; such a delimiter could never be captured.
		"bare \\S is ascii-only": `\S`,
		"negated \\s keeps \\v":  `[^\s\p{Z}]`,
	}
	for name, pat := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Panics(t, delimType(t, pat))
		})
	}
}

// The body starts where the delimiter ends, so a template cannot continue past
// it. Without this the trailing-position panic goes unexercised.
func TestBlockDelimArgMustEndTemplate(t *testing.T) {
	s := New()
	assert.Panics(t, func() {
		s.Node("banner {{ d:delim }} {{ msg:rest }}").BlockDelim("d")
	})
	assert.Panics(t, func() {
		s.Node("banner {{ d:delim }} trailing").BlockDelim("d")
	})
	assert.NotPanics(t, func() {
		s.Node("banner {{ kind:word }} {{ d:delim }}").BlockDelim("d")
	})
}

// Builder guards must hold for either call order.
func TestBlockDelimGuardsBothCallOrders(t *testing.T) {
	t.Run("list then block", func(t *testing.T) {
		s := New()
		n := s.Node("vlans {{ ids:rest }} {{ d:delim }}").List("ids", "uint")
		assert.Panics(t, func() { n.BlockDelim("d") })
	})
	t.Run("block then list", func(t *testing.T) {
		s := New()
		n := s.Node("vlans {{ ids:rest }} {{ d:delim }}").BlockDelim("d")
		assert.Panics(t, func() { n.List("ids", "uint") })
	})
	t.Run("section exit then block", func(t *testing.T) {
		s := New()
		n := s.Node("banner motd {{ d:delim }}").SectionExit("!")
		assert.Panics(t, func() { n.BlockDelim("d") })
	})
	t.Run("block then section exit", func(t *testing.T) {
		s := New()
		n := s.Node("banner motd {{ d:delim }}").BlockDelim("d")
		assert.Panics(t, func() { n.SectionExit("!") })
	})
}

// A block opener matches a prefix, which is more permissive than an anchored
// match. The text it keeps is the node's identity, so it must still bind the
// definition that produced it or the node is re-read as a different command.
func TestMatchChildOpenerRejectsPrefixASiblingClaims(t *testing.T) {
	s := New()
	s.Node("banner motd {{ x:word }}").Card(ZeroToN)
	s.Node("banner motd {{ d:delim }}").Card(ZeroToOne).BlockDelim("d")

	// "banner motd ^" binds the word definition, so the block definition must
	// not claim the opener and leave a node that re-reads as the other one.
	_, _, _, ok := MatchChildOpener(s.Roots, "banner motd ^ hi ^")
	assert.False(t, ok)

	// Without the collision the same opener binds the block definition.
	s2 := New()
	only := s2.Node("banner motd {{ d:delim }}").Card(ZeroToOne).BlockDelim("d")
	def, f, end, ok := MatchChildOpener(s2.Roots, "banner motd ^ hi ^")
	require.True(t, ok)
	assert.Equal(t, only, def)
	assert.Equal(t, "^", f["d"])
	assert.Equal(t, len("banner motd ^"), end)
}

// MatchChildOpener returns the whole line for a non-block definition, and the
// fields it reports must match what MatchChild reports for the same line.
func TestMatchChildOpenerAgreesWithMatchChild(t *testing.T) {
	s := New()
	s.Node("hostname {{ name:word }}").Card(ZeroToOne)
	def, f, end, ok := MatchChildOpener(s.Roots, "hostname sw1")
	require.True(t, ok)
	want, wf, wok := MatchChild(s.Roots, "hostname sw1")
	require.True(t, wok)
	assert.Equal(t, want, def)
	assert.Equal(t, wf, f)
	assert.Equal(t, len("hostname sw1"), end)
}

func TestNormalizeLineTrimsAndCollapses(t *testing.T) {
	assert.Equal(t, "a b", NormalizeLine("   a   b  "))
	assert.Equal(t, "a b", NormalizeLine("a\u0085b"))
	assert.Equal(t, "", NormalizeLine("  \t \n "))
}
