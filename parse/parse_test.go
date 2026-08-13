package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/schema"
)

func miniSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}").
		Card(schema.ZeroToOne)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	return s
}

func TestParseNesting(t *testing.T) {
	in := "interface Ethernet1/1\n" +
		"  ip address 10.0.0.1 255.255.255.0\n" +
		"  shutdown\n" +
		"vlan 10\n"
	d := diag.New()
	cfg := Parse(miniSchema(), in, Reject, d)

	require.False(t, d.HasErrors(), d.String())
	top := cfg.Root.Children
	require.Len(t, top, 2)
	assert.Equal(t, "interface Ethernet1/1", top[0].Text)
	assert.Equal(t, "Ethernet1/1", top[0].Fields["name"])
	require.Equal(t, 2, len(top[0].Children))
	assert.Equal(
		t,
		"ip address 10.0.0.1 255.255.255.0",
		top[0].Children[0].Text,
	)
	assert.Equal(t, "vlan 10", top[1].Text)
}

func TestParseWhitespaceNormalized(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		miniSchema(),
		"interface   Ethernet1/1\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "interface Ethernet1/1", cfg.Root.Children[0].Text)
}

func TestParseUnknownStrict(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		miniSchema(),
		"frobnicate the foo\n",
		Reject,
		d,
	)
	assert.True(t, d.HasErrors())
	assert.Empty(t, cfg.Root.Children) // dropped from tree
}

func TestParseUnknownLenient(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n" +
		"  flux-capacitor on\n" +
		"  shutdown\n"
	cfg := Parse(miniSchema(), in, Drop, d)
	assert.False(t, d.HasErrors()) // warnings only
	require.Len(t, cfg.Root.Children, 1)
	require.Equal(t, 1, len(cfg.Root.Children[0].Children))
	assert.Equal(t, "shutdown", cfg.Root.Children[0].Children[0].Text)
	assert.NotEmpty(t, d.Items)
}

func blockSchema() *schema.Schema {
	s := schema.New()
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("certificate {{ name:word }}").
		Card(schema.ZeroToN).BlockUntil("quit")
	iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
	iface.Child("mtu {{ m:uint }}")
	return s
}

func TestParseBlockDelim(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		blockSchema(),
		"banner motd ^\nhello  world\n\n  interface fake\n^\ninterface eth1\n  mtu 9000\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	top := cfg.Root.Children
	require.Len(t, top, 2)
	// body verbatim: internal spacing, blank line, and schema-looking lines kept
	assert.Equal(
		t,
		[]string{"hello  world", "", "  interface fake"},
		top[0].Block,
	)
	// parsing resumed cleanly after the terminator
	assert.Equal(t, "interface eth1", top[1].Text)
	require.Equal(t, 1, len(top[1].Children))
}

func TestParseBlockUntil(t *testing.T) {
	d := diag.New()
	cfg := Parse(blockSchema(),
		"certificate ca1\nMIIB\nAAAA\nquit\n", Reject, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, []string{"MIIB", "AAAA"}, cfg.Root.Children[0].Block)
}

func TestParseBlockEmptyBody(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		blockSchema(),
		"banner motd ^\n^\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	assert.Empty(t, cfg.Root.Children[0].Block)
}

func TestParseBlockUnterminated(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		blockSchema(),
		"banner motd ^\nhello\n",
		Reject,
		d,
	)
	assert.True(t, d.HasErrors())
	assert.Equal(
		t,
		[]string{"hello"},
		cfg.Root.Children[0].Block,
	) // body-so-far kept

	d2 := diag.New()
	Parse(
		blockSchema(),
		"banner motd ^\nhello\n",
		Drop,
		d2,
	)
	assert.True(
		t,
		d2.HasErrors(),
	)
}

func TestParseBlockTerminatorTrailingWhitespace(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		blockSchema(),
		"banner motd ^\nbody\n^  \nafter capture unknown\n",
		Drop,
		d,
	)
	assert.Equal(t, []string{"body"}, cfg.Root.Children[0].Block)
}

func TestParseBlockCRLF(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		blockSchema(),
		"banner motd ^\r\nhello\r\n^\r\ninterface eth1\r\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	top := cfg.Root.Children
	require.Len(t, top, 2)
	assert.Equal(t, []string{"hello\r"}, top[0].Block)
	assert.Equal(t, "interface eth1", top[1].Text)
}

func TestParseIndentAfterBlockStrict(t *testing.T) {
	// A deeper-indented line after a block terminator errors like it would
	// Keep deeper input under an ordinary leaf isolated instead of reparenting it.
	d := diag.New()
	cfg := Parse(blockSchema(), "banner motd ^\nbody\n^\n  interface eth1\n",
		Reject, d)
	assert.True(t, d.HasErrors())
	require.Len(t, cfg.Root.Children, 1) // only the banner survives
}

func TestParseLineNumbers(t *testing.T) {
	// Blank lines and skipped input still count: numbers are positions in
	// the imported text, not indices into what survived.
	in := "\ninterface Ethernet1/1\n\n  shutdown\nvlan 10\n"
	d := diag.New()
	cfg := Parse(miniSchema(), in, Reject, d)
	require.False(t, d.HasErrors(), d.String())
	top := cfg.Root.Children
	assert.Equal(t, 2, top[0].Line)
	assert.Equal(t, 4, top[0].Children[0].Line)
	assert.Equal(t, 5, top[1].Line)
}

func TestParseUnknownDiagCarriesLine(t *testing.T) {
	d := diag.New()
	Parse(
		miniSchema(),
		"vlan 10\nbogus command\n",
		Reject,
		d,
	)
	require.True(t, d.HasErrors())
	assert.Equal(t, 2, d.Items[0].Line)
	assert.Contains(t, d.String(), "2: error: unknown command")

	ld := diag.New()
	Parse(miniSchema(), "bogus\n", Drop, ld)
	assert.Equal(t, 1, ld.Items[0].Line)
	// The aggregate "N nodes dropped" summary has no single line.
	assert.Equal(t, 0, ld.Items[1].Line)
}
