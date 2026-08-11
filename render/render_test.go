package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/schema"
)

func miniSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}")
	iface.Child("shutdown")
	return s
}

func TestRenderCanonicalRoundTrip(t *testing.T) {
	in := "interface Ethernet1/1\n" +
		"  ip address 10.0.0.1 255.255.255.0\n" +
		"  shutdown\n"
	d := diag.New()
	cfg := parse.Parse(miniSchema(), in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, in, Render(cfg))
}

func TestRenderNormalizesMessyInput(t *testing.T) {
	messy := "interface   Ethernet1/1\n" +
		"    ip address 10.0.0.1    255.255.255.0\n"
	canonical := "interface Ethernet1/1\n" +
		"  ip address 10.0.0.1 255.255.255.0\n"
	d := diag.New()
	cfg := parse.Parse(miniSchema(), messy, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	out := Render(cfg)
	assert.Equal(t, canonical, out)
	// idempotent: re-parsing+rendering the canonical form is stable
	cfg2 := parse.Parse(miniSchema(), out, diag.Policy{Strict: true}, d)
	assert.Equal(t, canonical, Render(cfg2))
}

func TestRenderBlock(t *testing.T) {
	s := schema.New()
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
	iface.Child("mtu {{ m:uint }}")

	d := diag.New()
	in := "banner motd ^\nhello  world\n\n  deep\n^\ninterface eth1\n  mtu 9000\n"
	cfg := parse.Parse(s, in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, in, Render(cfg)) // byte-exact through the block
}
