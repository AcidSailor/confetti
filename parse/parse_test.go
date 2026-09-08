package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/render"
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
	assert.Empty(t, cfg.Root.Children)
}

func TestParseUnknownLenient(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n" +
		"  flux-capacitor on\n" +
		"  shutdown\n"
	cfg := Parse(miniSchema(), in, Drop, d)
	assert.False(t, d.HasErrors())
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
	assert.Equal(
		t,
		[]string{"hello  world", "", "  interface fake"},
		top[0].Block,
	)
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
	d := diag.New()
	cfg := Parse(blockSchema(), "banner motd ^\nbody\n^\n  interface eth1\n",
		Reject, d)
	assert.True(t, d.HasErrors())
	require.Len(t, cfg.Root.Children, 1) // only the banner survives
}

func TestParseLineNumbers(t *testing.T) {
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

// inlineBlockSchema carries banner text on the opener so a terminator can land on the opening line.
func inlineBlockSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("banner motd {{ delim:word }}{{ first:text }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("certificate {{ name:rest }}").
		Card(schema.ZeroToN).BlockUntil("quit")
	s.Node("hostname {{ name:word }}").Card(schema.ZeroToOne)
	return s
}

func TestParseBlockInlineClose(t *testing.T) {
	cases := []struct{ name, opener, want string }{
		{
			"body",
			"banner motd ^ Authorized users only. ^",
			"banner motd ^ Authorized users only.",
		},
		{"empty body", "banner motd ^^", "banner motd ^"},
		// Cutting only the last terminator leaves a node that re-parses shorter.
		{"trailing terminators", "banner motd ^ hi ^ ^", "banner motd ^ hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := diag.New()
			cfg := Parse(
				inlineBlockSchema(),
				tc.opener+"\nhostname sw1\n",
				Reject,
				d,
			)
			require.False(t, d.HasErrors(), d.String())
			top := cfg.Root.Children
			require.Len(t, top, 2)
			assert.Equal(t, tc.want, top[0].Text)
			assert.Equal(t, "^", top[0].Fields["delim"])
			assert.Equal(t, []string{}, top[0].Block)
			assert.Equal(t, "hostname sw1", top[1].Text)
		})
	}
}

func TestParseBlockInlineCloseEqualsMultiLine(t *testing.T) {
	s := inlineBlockSchema()
	one := Parse(
		s,
		"banner motd ^ Authorized users only. ^\n",
		Reject,
		diag.New(),
	)
	multi := Parse(
		s,
		"banner motd ^ Authorized users only.\n^\n",
		Reject,
		diag.New(),
	)
	assert.True(t, one.Root.Children[0].SameValue(multi.Root.Children[0]))
}

func TestParseBlockInlineCloseMultiLineUnchanged(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		inlineBlockSchema(),
		"banner motd ^ line one\nline two ^\n^\nhostname sw1\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	top := cfg.Root.Children
	require.Len(t, top, 2)
	assert.Equal(t, []string{"line two ^"}, top[0].Block)
}

func TestParseBlockOpenerWithoutTerminatorStaysOpen(t *testing.T) {
	d := diag.New()
	Parse(inlineBlockSchema(), "banner motd ^ open\nhostname sw1\n", Reject, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "block not terminated before end of input")
}

func TestParseBlockUntilOpenerIgnoresTerminator(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		inlineBlockSchema(),
		"certificate ca quit\nMIIB\nquit\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "certificate ca quit", cfg.Root.Children[0].Text)
	assert.Equal(t, []string{"MIIB"}, cfg.Root.Children[0].Block)
}

func TestParseBlockUntilOpenerWithTerminatorInText(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		inlineBlockSchema(),
		"certificate quit quit\nMIIB\nquit\n",
		Reject,
		d,
	)
	// Only BlockDelim closes on its opener, even when the head re-binds.
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "certificate quit quit", cfg.Root.Children[0].Text)
	assert.Equal(t, []string{"MIIB"}, cfg.Root.Children[0].Block)
}

// nearCloseSchema requires at least one character of banner text, so an empty one-line body cannot re-bind.
func nearCloseSchema() *schema.Schema {
	s := schema.New()
	s.Node("banner motd {{ delim:word }}{{ body:rest }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("hostname {{ name:word }}").Card(schema.ZeroToOne)
	return s
}

func TestParseBlockInlineCloseRoundTrips(t *testing.T) {
	s := inlineBlockSchema()
	for _, in := range []string{
		"banner motd ^ Authorized users only. ^\n",
		"banner motd ^ hi ^ ^\n",
		"banner motd ^^^\n",
		"banner motd ^^\n",
	} {
		first := render.Render(Parse(s, in, Reject, diag.New()))
		d := diag.New()
		second := render.Render(Parse(s, first, Reject, d))
		require.False(t, d.HasErrors(), "%q: %s", in, d.String())
		assert.Equal(t, first, second, "render is not idempotent for %q", in)
	}
}

func TestParseBlockInlineCloseRejectsDifferentDef(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	// A sibling block with the same delimiter arg makes the terminator check agree.
	s.Node("banner motd {{ delim:word }}{{ msg:text }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("banner motd {{ delim:word }} hello").
		Card(schema.ZeroToOne).BlockDelim("delim")
	want := s.Roots[0]
	d := diag.New()
	cfg := Parse(s, "banner motd ^ hello ^\nhostname sw1\n", Reject, d)
	// The shorter text binds the sibling, so the opener may not close on itself.
	require.Len(t, cfg.Root.Children, 1)
	got := cfg.Root.Children[0]
	assert.Equal(t, want, got.Def)
	assert.Equal(t, "banner motd ^ hello ^", got.Text)
	assert.Equal(t, []string{"hostname sw1"}, got.Block)
}

func TestParseBlockInlineCloseRejectsDifferentTerminator(t *testing.T) {
	s := schema.New()
	s.Node("banner {{ msg:rest }} {{ delim:word }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	d := diag.New()
	cfg := Parse(s, "banner ^ hello ^\n", Reject, d)
	// The shorter text re-binds delim to "hello", so the delimiter would change.
	assert.Equal(t, "banner ^ hello ^", cfg.Root.Children[0].Text)
	assert.Equal(t, "^", cfg.Root.Children[0].Fields["delim"])
}

func TestParseBlockInlineCloseMultiCharDelimiter(t *testing.T) {
	s := schema.New()
	s.Node("banner motd {{ delim:word }} {{ msg:rest }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("hostname {{ name:word }}").Card(schema.ZeroToOne)
	d := diag.New()
	cfg := Parse(s, "banner motd EOF hello EOF\nhostname sw1\n", Reject, d)
	require.False(t, d.HasErrors(), d.String())
	top := cfg.Root.Children
	require.Len(t, top, 2)
	assert.Equal(t, "banner motd EOF hello", top[0].Text)
	assert.Equal(t, "EOF", top[0].Fields["delim"])
	assert.Equal(t, "hostname sw1", top[1].Text)
}

func TestParseBlockInlineCloseNested(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
	iface.Child("banner login {{ delim:word }}{{ msg:text }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	iface.Child("mtu {{ size:uint }}").Card(schema.ZeroToOne)
	d := diag.New()
	cfg := Parse(
		s,
		"interface eth1\n  banner login ^ hi ^\n  mtu 9000\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	kids := cfg.Root.Children[0].Children
	require.Len(t, kids, 2)
	assert.Equal(t, "banner login ^ hi", kids[0].Text)
	assert.Equal(t, "mtu 9000", kids[1].Text)
}

func TestParseIndentAfterInlineCloseStrict(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		inlineBlockSchema(),
		"banner motd ^ hi ^\n  hostname sw1\n",
		Reject,
		d,
	)
	// A block definition has no children, so a deeper line is unknown.
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `2: error: unknown command: "hostname sw1"`)
	require.Len(t, cfg.Root.Children, 1)
}

func TestParseBlockNearCloseWarns(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		nearCloseSchema(),
		"banner motd ^^\nhostname sw1\n^\n",
		Reject,
		d,
	)
	// Without the Warning the opener would absorb hostname sw1 silently.
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "1: warning:")
	assert.Contains(t, d.String(), "ends with block terminator")
	require.Len(t, cfg.Root.Children, 1)
	assert.Equal(t, []string{"hostname sw1"}, cfg.Root.Children[0].Block)
}

func TestParseBlockPlainOpenerDoesNotWarn(t *testing.T) {
	d := diag.New()
	Parse(blockSchema(), "banner motd ^\nbody\n^\ninterface eth1\n", Reject, d)
	// Every BlockDelim opener ends with its delimiter; only a second one is ambiguous.
	assert.Empty(t, d.Items)
}
