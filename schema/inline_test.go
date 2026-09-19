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

// The one-line form must bind the node's definition against all siblings, in either declaration order.
func TestInlineBlockRejectsDifferentDef(t *testing.T) {
	for _, generalFirst := range []bool{true, false} {
		s := textSchema(t)
		if generalFirst {
			s.Node("banner motd {{ delim:word }}{{ first:text }}").
				BlockDelim("delim")
		}
		s.Node("banner motd {{ delim:word }} hello ^").BlockDelim("delim")
		if !generalFirst {
			s.Node("banner motd {{ delim:word }}{{ first:text }}").
				BlockDelim("delim")
		}
		n := inlineNode(t, s, "banner motd ^ hello")
		require.Equal(
			t,
			"banner motd {{ delim:word }}{{ first:text }}",
			n.Def.Template,
		)
		_, ok := n.InlineBlock()
		assert.False(t, ok)
	}
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
