package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/value"
)

func mustSpec(t *testing.T, tmpl string) *matchSpec {
	t.Helper()
	reg := value.NewRegistry()
	testtypes.Fill(reg)
	s, err := compileSpec(tmpl, reg)
	require.NoError(t, err)
	return s
}

func TestMatchLiteralOnly(t *testing.T) {
	s := mustSpec(t, "shutdown")
	f, ok := s.Match("shutdown")
	assert.True(t, ok)
	assert.Empty(t, f)
	_, ok = s.Match("no shutdown")
	assert.False(t, ok)
}

func TestMatchCaptures(t *testing.T) {
	s := mustSpec(t, "ip address {{ ip:ipv4 }} {{ mask:ipv4 }}")
	f, ok := s.Match("ip address 10.0.0.1 255.255.255.0")
	require.True(t, ok)
	assert.Equal(t, "10.0.0.1", f["ip"])
	assert.Equal(t, "255.255.255.0", f["mask"])
}

func TestMatchDefaultTypeIsWord(t *testing.T) {
	s := mustSpec(t, "vrf member {{ name }}")
	f, ok := s.Match("vrf member RED")
	require.True(t, ok)
	assert.Equal(t, "RED", f["name"])
	assert.Equal(t, "word", s.ArgType("name"))
}

func TestRenderIsInverseOfMatch(t *testing.T) {
	s := mustSpec(t, "ip address {{ ip:ipv4 }} {{ mask:ipv4 }}")
	line := "ip address 10.0.0.1 255.255.255.0"
	f, ok := s.Match(line)
	require.True(t, ok)
	assert.Equal(t, line, s.Render(f))
}

func TestSpecificity(t *testing.T) {
	plain := mustSpec(t, "ip address {{ ip:ipv4 }}")
	secondary := mustSpec(t, "ip address {{ ip:ipv4 }} secondary")
	assert.Greater(t, secondary.litLen, plain.litLen)
}

func TestUnknownTypeErrors(t *testing.T) {
	_, err := compileSpec("foo {{ x:bogus }}", value.NewRegistry())
	assert.Error(t, err)
}

func TestDuplicateCaptureNameErrors(t *testing.T) {
	_, err := compileSpec("pair {{ x }} {{ x }}", value.NewRegistry())
	assert.Error(t, err)
}

func TestLazify(t *testing.T) {
	tests := []struct{ in, want string }{
		{`\S+`, `\S+?`},       // unescaped quantifier: made lazy
		{`\S*`, `\S*?`},       // both quantifier bytes covered
		{`\S+\+`, `\S+\+`},    // escaped '+': required literal, untouched
		{`\S+\\+`, `\S+\\+?`}, // even backslash run: still a quantifier
		{`a{1,3}`, `a{1,3}`},  // trailing '}' is ambiguous: untouched
		{`ab?`, `ab?`},        // already-greedy '?': untouched
		{``, ``},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, lazify(tt.in), tt.in)
	}
}

// Regression: a "}}" appearing in literal text before a "{{" must not be
// mistaken for the capture's closing delimiter.
func TestLiteralBracesBeforeCapture(t *testing.T) {
	s := mustSpec(t, "a}}b {{ x }}")
	f, ok := s.Match("a}}b VALUE")
	require.True(t, ok)
	assert.Equal(t, "VALUE", f["x"])
	assert.Equal(t, "a}}b VALUE", s.Render(f))
}
