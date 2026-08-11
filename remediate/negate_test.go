package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/schema"
)

func TestNegateNoPrefixDefault(t *testing.T) {
	s := schema.New()
	n := s.Node("description {{ text:rest }}")
	f, ok := n.MatchLine("description Customer A")
	require.True(t, ok)
	assert.Equal(t, "no description Customer A", negateLine(n, f, n.Render(f)))
}

func TestNegateStripsExistingNoPrefix(t *testing.T) {
	s := schema.New()
	n := s.Node("no shutdown")
	f, ok := n.MatchLine("no shutdown")
	require.True(t, ok)
	// negating "no shutdown" yields "shutdown", never "no no shutdown"
	assert.Equal(t, "shutdown", negateLine(n, f, n.Render(f)))
}

func TestNegateDefaultPrefix(t *testing.T) {
	s := schema.New()
	n := s.Node("maximum-paths ibgp {{ n:uint }}").NegateDefault()
	f, ok := n.MatchLine("maximum-paths ibgp 4")
	require.True(t, ok)
	assert.Equal(
		t,
		"default maximum-paths ibgp 4",
		negateLine(n, f, n.Render(f)),
	)
}

func TestNegateFuncDispatch(t *testing.T) {
	s := schema.New()
	n := s.Node("crypto key {{ name:rest }}").
		NegateFunc(func(f map[string]string, rendered string) string {
			return "crypto key zeroize " + f["name"]
		})
	f, ok := n.MatchLine("crypto key mykey")
	require.True(t, ok)
	assert.Equal(t, "crypto key zeroize mykey", negateLine(n, f, n.Render(f)))
}

func TestNegateTemplateLiteral(t *testing.T) {
	s := schema.New()
	n := s.Node("description {{ text:rest }}").NegateAs("no description")
	f, ok := n.MatchLine("description Customer A")
	require.True(t, ok)
	// capture-free template renders to its literal
	assert.Equal(t, "no description", negateLine(n, f, n.Render(f)))
}

func TestNegateTemplateInterpolates(t *testing.T) {
	s := schema.New()
	n := s.Node("switchport trunk allowed vlan {{ vlans:rest }}").
		NegateAs("switchport trunk allowed vlan remove {{ vlans }}")
	f, ok := n.MatchLine("switchport trunk allowed vlan 10")
	require.True(t, ok)
	assert.Equal(
		t,
		"switchport trunk allowed vlan remove 10",
		negateLine(n, f, n.Render(f)),
	)
}

func TestInterpolateCloseBraceBeforeOpen(t *testing.T) {
	// A literal "}}" occurring before the first "{{" must be treated as literal
	// text, not trigger a slice-bounds panic. Regression: the close-delimiter
	// search started at index 0 instead of just after the opener.
	got := schema.Interpolate(
		"close }} then open {{ a }}",
		map[string]string{"a": "X"},
	)
	assert.Equal(t, "close }} then open X", got)
}

func TestNegateNilDefIsDefensive(t *testing.T) {
	// def==nil cannot occur via Import (the parser drops unmatched lines), but the
	// negation must not panic if a node is hand-built or injected by a transform.
	assert.Equal(
		t,
		"no flux-capacitor on",
		negateLine(nil, nil, "flux-capacitor on"),
	)
}
