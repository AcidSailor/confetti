package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/value"
)

func inlineNode(t *testing.T, s *Schema, line string) *Node {
	t.Helper()
	def, fields, ok := MatchChild(s.Roots, line)
	require.True(t, ok, line)
	n := NewNode(line)
	n.Def, n.Fields, n.Block = def, fields, []string{}
	NewConfig(s).Root.AddChild(n)
	return n
}

func textSchema(t *testing.T) *Schema {
	t.Helper()
	s := New()
	require.NoError(
		t,
		s.Registry.Register(value.Type{Name: "text", Pattern: `.*`}),
	)
	return s
}

func TestInlineBlock(t *testing.T) {
	s := textSchema(t)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").BlockDelim("delim")
	line, ok := inlineNode(t, s, "banner motd ^ hi").InlineBlock()
	assert.True(t, ok)
	assert.Equal(t, "banner motd ^ hi ^", line)
}

func TestInlineBlockRejectsBody(t *testing.T) {
	s := textSchema(t)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").BlockDelim("delim")
	n := inlineNode(t, s, "banner motd ^ hi")
	n.Block = []string{""}
	_, ok := n.InlineBlock()
	assert.False(t, ok)
}

// A hand-built opener that ends with its terminator would lose it on reparse.
func TestInlineBlockRejectsOpenerEndingWithTerminator(t *testing.T) {
	s := textSchema(t)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").BlockDelim("delim")
	_, ok := inlineNode(t, s, "banner motd ^ hi ^").InlineBlock()
	assert.False(t, ok)
}

// A more specific sibling claims the one-line form even though the opener binds this definition.
func TestInlineBlockRejectsDifferentDef(t *testing.T) {
	s := textSchema(t)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").BlockDelim("delim")
	s.Node("banner motd {{ delim:word }} hello ^").BlockDelim("delim")
	n := inlineNode(t, s, "banner motd ^ hello")
	require.Equal(
		t,
		"banner motd {{ delim:word }}{{ first:text }}",
		n.Def.Template,
	)
	_, ok := n.InlineBlock()
	assert.False(t, ok)
}

// Declaration order alone decides an equal-specificity tie, so it decides whether
// the one-line form is safe. The competitor matches only the closed line, so the
// opener binds this definition in both orders.
func TestInlineBlockEqualSpecificityFollowsDeclarationOrder(t *testing.T) {
	for _, competitorFirst := range []bool{true, false} {
		s := textSchema(t)
		require.NoError(
			t,
			s.Registry.Register(value.Type{Name: "caret", Pattern: `.*\^`}),
		)
		competitor := func() {
			s.Node("banner motd {{ delim:word }}{{ closed:caret }}").
				BlockDelim("delim")
		}
		if competitorFirst {
			competitor()
		}
		def := s.Node("banner motd {{ delim:word }}{{ first:text }}").
			BlockDelim("delim")
		if !competitorFirst {
			competitor()
		}
		require.Equal(t, s.Roots[0].spec.litLen, s.Roots[1].spec.litLen)

		n := inlineNode(t, s, "banner motd ^ hello")
		require.Same(t, def, n.Def)
		line, ok := n.InlineBlock()
		if competitorFirst {
			// The closed line would bind the competitor, so it stays multi-line.
			assert.False(t, ok)
			continue
		}
		assert.True(t, ok)
		assert.Equal(t, "banner motd ^ hello ^", line)
	}
}

// An empty terminator strips nothing, so it would never converge.
func TestInlineClosePanicsWithoutBlockDelim(t *testing.T) {
	s := textSchema(t)
	def := s.Node("hostname {{ name:word }}")
	assert.Panics(t, func() {
		InlineClose(s.Roots, def, "hostname sw1", "")
	})
}

// Fields built outside the parser can hold spacing that parsing would collapse.
func TestInlineBlockRejectsNonNormalizedFields(t *testing.T) {
	s := textSchema(t)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").BlockDelim("delim")
	n := inlineNode(t, s, "banner motd ^ hi")
	n.Fields["first"] = "  hi"
	_, ok := n.InlineBlock()
	assert.False(t, ok)
}

// A delimiter field emptied outside the parser degrades instead of panicking.
func TestInlineBlockRejectsEmptyTerminator(t *testing.T) {
	s := textSchema(t)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").BlockDelim("delim")
	n := inlineNode(t, s, "banner motd ^ hi")
	n.Fields["delim"] = ""
	_, ok := n.InlineBlock()
	assert.False(t, ok)
}

func TestInlineBlockDetachedNestedNodeFallsBack(t *testing.T) {
	s := textSchema(t)
	sec := s.Node("line {{ name:word }}")
	def := sec.Child("banner exec {{ delim:word }}{{ first:text }}").
		BlockDelim("delim")
	fields, ok := BindsDef(sec.Children, def, "banner exec ^ hi")
	require.True(t, ok)
	n := NewNode("banner exec ^ hi")
	n.Def, n.Fields = def, fields
	_, ok = n.InlineBlock()
	assert.False(t, ok)
}
