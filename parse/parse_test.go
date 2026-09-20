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
	s.Node("banner motd {{ delim:delim }}").
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
	// The body runs from the opener's delimiter to the next one, so it starts
	// and ends with the empty text beside each delimiter.
	assert.Equal(
		t,
		[]string{"", "hello  world", "", "  interface fake", ""},
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

func TestParseBlockNewlineBody(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		blockSchema(),
		"banner motd ^\n^\n",
		Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	// A newline between the delimiters is a body; only "^^" is the empty form.
	assert.Equal(t, []string{"", ""}, cfg.Root.Children[0].Block)
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
	// The block never closed, so rendering it would invent a terminator that
	// reads back as a different block. The node is dropped instead.
	assert.Empty(t, cfg.Root.Children)

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
	assert.Equal(t, []string{"", "body", ""}, cfg.Root.Children[0].Block)
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
	assert.Equal(t, []string{"\r", "hello\r", ""}, top[0].Block)
	assert.Equal(t, "interface eth1", top[1].Text)
	// The body keeps its carriage returns, but render ends every line it writes
	// itself with a bare newline, so CRLF input is canonical rather than
	// byte-exact. Pinned because goldens are byte-exact contracts.
	out := render.Render(cfg)
	assert.Equal(t, "banner motd ^\r\nhello\r\n^\ninterface eth1\n", out)
	assert.Equal(
		t,
		out,
		render.Render(Parse(blockSchema(), out, Reject, diag.New())),
	)
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

// oneLineBlockSchema holds a delimited banner alongside a BlockUntil
// certificate and a plain hostname, so tests can check a block against
// unrelated siblings. The banner needs no special template to close on its
// opening line; that follows from the forward scan.
func oneLineBlockSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("banner motd {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("certificate {{ name:rest }}").
		Card(schema.ZeroToN).BlockUntil("quit")
	s.Node("hostname {{ name:word }}").Card(schema.ZeroToOne)
	return s
}

func TestParseBlockClosesOnOpeningLine(t *testing.T) {
	cases := []struct{ name, opener, body string }{
		{
			"body",
			"banner motd ^ Authorized users only. ^",
			" Authorized users only. ",
		},
		// Body text is block content, so its spacing survives byte-exact.
		{"spacing is kept", "banner motd ^  hi  ^", "  hi  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := diag.New()
			cfg := Parse(
				oneLineBlockSchema(),
				tc.opener+"\nhostname sw1\n",
				Reject,
				d,
			)
			require.False(t, d.HasErrors(), d.String())
			top := cfg.Root.Children
			require.Len(t, top, 2)
			assert.Equal(t, "banner motd ^", top[0].Text)
			assert.Equal(t, "^", top[0].Fields["delim"])
			assert.Equal(t, []string{tc.body}, top[0].Block)
			assert.Equal(t, "hostname sw1", top[1].Text)
		})
	}
}

// A device ends the banner at the second delimiter, so trailing text is an
// error. The severity decides whether Import rejects the configuration, so it is
// asserted with the message.
func TestParseBlockTextAfterTerminator(t *testing.T) {
	d := diag.New()
	cfg := Parse(oneLineBlockSchema(), "banner motd ^ a ^ b ^\n", Reject, d)
	require.True(t, d.HasErrors(), d.String())
	assert.Equal(
		t,
		`1: error: banner motd ^: "b ^" follows the closing delimiter "^" and was dropped`+"\n",
		d.String(),
	)
	assert.Equal(t, []string{" a "}, cfg.Root.Children[0].Block)
}

// The terminator can close on a later line, and trailing text there is the same
// error. Without this the whole multi-line tail path goes unasserted.
func TestParseBlockTextAfterTerminatorOnBodyLine(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		oneLineBlockSchema(),
		"banner motd ^\nbody ^ junk\nhostname sw1\n",
		Reject,
		d,
	)
	require.True(t, d.HasErrors(), d.String())
	assert.Equal(
		t,
		`2: error: banner motd ^: "junk" follows the closing delimiter "^" and was dropped`+"\n",
		d.String(),
	)
	require.Len(t, cfg.Root.Children, 2)
	assert.Equal(t, []string{"", "body "}, cfg.Root.Children[0].Block)
	// The tail must not leak into the node that follows it.
	assert.Equal(t, "hostname sw1", cfg.Root.Children[1].Text)
}

// Whitespace after the closing delimiter cannot change how a device reads the
// line, so it is dropped without a diagnostic.
func TestParseBlockWhitespaceAfterTerminatorIsSilent(t *testing.T) {
	d := diag.New()
	cfg := Parse(oneLineBlockSchema(), "banner motd ^ hi ^   \n", Reject, d)
	assert.Empty(t, d.Items)
	assert.Equal(t, []string{" hi "}, cfg.Root.Children[0].Block)
}

// A device accepts the empty form and then omits it from its configuration.
// The drop is reported, or a caller cannot tell it from input without a banner.
func TestParseBlockEmptyInlineIsDropped(t *testing.T) {
	s := oneLineBlockSchema()
	d := diag.New()
	cfg := Parse(s, "banner motd ^^\nhostname sw1\n", Reject, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(
		t,
		"1: warning: banner motd ^: empty delimited block dropped;"+
			" a device omits it from its running configuration\n",
		d.String(),
	)
	require.Len(t, cfg.Root.Children, 1)
	assert.Equal(t, "hostname sw1", cfg.Root.Children[0].Text)
}

// An ordinary block reports nothing at all; a spurious warning on the common
// path would otherwise go unnoticed.
func TestParseBlockPlainOpenerIsSilent(t *testing.T) {
	d := diag.New()
	Parse(oneLineBlockSchema(), "banner motd ^\nhello\n^\n", Reject, d)
	assert.Empty(t, d.Items)
}

// The two forms hold different text, so they are different banners.
func TestParseBlockInlineDiffersFromMultiLine(t *testing.T) {
	s := oneLineBlockSchema()
	one := Parse(s, "banner motd ^ hi ^\n", Reject, diag.New())
	multi := Parse(s, "banner motd ^\nhi\n^\n", Reject, diag.New())
	assert.Equal(t, []string{" hi "}, one.Root.Children[0].Block)
	assert.Equal(t, []string{"", "hi", ""}, multi.Root.Children[0].Block)
	assert.False(t, one.Root.Children[0].SameValue(multi.Root.Children[0]))
}

func TestParseBlockMultiLineUnchanged(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		oneLineBlockSchema(),
		"banner motd ^ line one\nline two ^\n^\nhostname sw1\n",
		Reject,
		d,
	)
	// The delimiter closes the block mid-line, so the next "^" is a stray line.
	assert.Contains(t, d.String(), `unknown command: "^"`)
	assert.Equal(
		t,
		[]string{" line one", "line two "},
		cfg.Root.Children[0].Block,
	)
}

func TestParseBlockOpenerWithoutTerminatorStaysOpen(t *testing.T) {
	d := diag.New()
	Parse(oneLineBlockSchema(), "banner motd ^ open\nhostname sw1\n", Reject, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "block not terminated before end of input")
}

func TestParseBlockUntilOpenerIgnoresTerminator(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		oneLineBlockSchema(),
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
		oneLineBlockSchema(),
		"certificate quit quit\nMIIB\nquit\n",
		Reject,
		d,
	)
	// Only BlockDelim closes on its opener, even when the head re-binds.
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "certificate quit quit", cfg.Root.Children[0].Text)
	assert.Equal(t, []string{"MIIB"}, cfg.Root.Children[0].Block)
}

// Each one-line form renders to a fixed text and reports fixed diagnostics.
// Asserting only idempotence would pass vacuously for the forms that render to
// nothing, and would hide the ones that now report an Error.
func TestParseBlockOneLineForms(t *testing.T) {
	cases := []struct{ name, in, want, diags string }{
		{
			name: "body",
			in:   "banner motd ^ Authorized users only. ^\n",
			want: "banner motd ^ Authorized users only. ^\n",
		},
		{
			name: "third delimiter is trailing text",
			in:   "banner motd ^ hi ^ ^\n",
			want: "banner motd ^ hi ^\n",
			diags: `1: error: banner motd ^: "^" follows the closing` +
				" delimiter \"^\" and was dropped\n",
		},
		{
			name: "empty body then a stray delimiter",
			in:   "banner motd ^^^\n",
			want: "",
			diags: `1: error: banner motd ^: "^" follows the closing` +
				" delimiter \"^\" and was dropped\n" +
				"1: warning: banner motd ^: empty delimited block dropped;" +
				" a device omits it from its running configuration\n",
		},
		{
			name: "empty body",
			in:   "banner motd ^^\n",
			want: "",
			diags: "1: warning: banner motd ^: empty delimited block dropped;" +
				" a device omits it from its running configuration\n",
		},
	}
	s := oneLineBlockSchema()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := diag.New()
			first := render.Render(Parse(s, tc.in, Reject, d))
			assert.Equal(t, tc.want, first)
			assert.Equal(t, tc.diags, d.String())

			// Re-parsing the render must be clean and reach the same text.
			rd := diag.New()
			second := render.Render(Parse(s, first, Reject, rd))
			assert.Empty(t, rd.Items, rd.String())
			assert.Equal(t, first, second)
		})
	}
}

// NormalizeLine folds every unicode.IsSpace rune, so rawOffset must use the
// same test to find where the body starts on the opener's raw line.
func TestParseBlockUnicodeSpaceInOpener(t *testing.T) {
	s := oneLineBlockSchema()
	for _, sp := range []string{"\u0085", " ", " ", "　"} {
		t.Run(sp, func(t *testing.T) {
			in := "banner motd" + sp + "^ hi ^\n"
			d := diag.New()
			cfg := Parse(s, in, Reject, d)
			require.Empty(t, d.Items, d.String())
			require.Len(t, cfg.Root.Children, 1)
			assert.Equal(t, []string{" hi "}, cfg.Root.Children[0].Block)
			assert.Equal(
				t,
				"banner motd ^ hi ^\n",
				render.Render(cfg),
			)
		})
	}
}

// An empty or unterminated block nested in a section drops only itself.
func TestParseBlockNestedDrops(t *testing.T) {
	newSchema := func() *schema.Schema {
		s := schema.New()
		testtypes.Fill(s.Registry)
		iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
		iface.Child("banner login {{ delim:delim }}").
			Card(schema.ZeroToOne).BlockDelim("delim")
		iface.Child("mtu {{ size:uint }}").Card(schema.ZeroToOne)
		return s
	}
	t.Run("empty body keeps its siblings", func(t *testing.T) {
		d := diag.New()
		cfg := Parse(
			newSchema(),
			"interface eth1\n  banner login ^^\n  mtu 9000\n",
			Reject,
			d,
		)
		require.False(t, d.HasErrors(), d.String())
		assert.Contains(t, d.String(), "empty delimited block dropped")
		kids := cfg.Root.Children[0].Children
		require.Len(t, kids, 1)
		assert.Equal(t, "mtu 9000", kids[0].Text)
	})
	t.Run("unterminated keeps its section", func(t *testing.T) {
		d := diag.New()
		cfg := Parse(
			newSchema(),
			"interface eth1\n  banner login ^\nunterminated\n",
			Reject,
			d,
		)
		assert.True(t, d.HasErrors())
		assert.Contains(t, d.String(), "captured lines were dropped")
		require.Len(t, cfg.Root.Children, 1)
		assert.Equal(t, "interface eth1", cfg.Root.Children[0].Text)
		assert.Empty(t, cfg.Root.Children[0].Children)
	})
}

func TestParseBlockOneLineNested(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
	iface.Child("banner login {{ delim:delim }}").
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
	assert.Equal(t, "banner login ^", kids[0].Text)
	assert.Equal(t, "mtu 9000", kids[1].Text)
}

func TestParseIndentAfterOneLineBlockStrict(t *testing.T) {
	d := diag.New()
	cfg := Parse(
		oneLineBlockSchema(),
		"banner motd ^ hi ^\n  hostname sw1\n",
		Reject,
		d,
	)
	// A block definition has no children, so a deeper line is unknown.
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `2: error: unknown command: "hostname sw1"`)
	require.Len(t, cfg.Root.Children, 1)
}
