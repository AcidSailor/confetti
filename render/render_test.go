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
	cfg := parse.Parse(miniSchema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, in, Render(cfg))
}

func TestRenderNormalizesMessyInput(t *testing.T) {
	messy := "interface   Ethernet1/1\n" +
		"    ip address 10.0.0.1    255.255.255.0\n"
	canonical := "interface Ethernet1/1\n" +
		"  ip address 10.0.0.1 255.255.255.0\n"
	d := diag.New()
	cfg := parse.Parse(miniSchema(), messy, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	out := Render(cfg)
	assert.Equal(t, canonical, out)
	// Parsing and rendering canonical output must preserve it.
	cfg2 := parse.Parse(miniSchema(), out, parse.Reject, d)
	assert.Equal(t, canonical, Render(cfg2))
}

func TestRenderBlock(t *testing.T) {
	s := schema.New()
	s.Node("banner motd {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
	iface.Child("mtu {{ m:uint }}")

	d := diag.New()
	in := "banner motd ^\nhello  world\n\n  deep\n^\ninterface eth1\n  mtu 9000\n"
	cfg := parse.Parse(s, in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, in, Render(cfg)) // byte-exact through the block
}

func inlineSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("banner motd {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	s.Node("hostname {{ name:word }}").Card(schema.ZeroToOne)
	sec := s.Node("line {{ name:word }}").Card(schema.ZeroToN)
	sec.Child("banner exec {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	return s
}

func TestRenderBlockInline(t *testing.T) {
	// Every form renders back byte-exact: the body is the text between the
	// two delimiters, so its line breaks are part of the banner.
	cases := []struct{ name, in, want string }{
		{
			"one line",
			"banner motd # one line banner #\nhostname sw1\n",
			"banner motd # one line banner #\nhostname sw1\n",
		},
		{
			"terminator on the next line is a different banner",
			"banner motd # one line banner\n#\nhostname sw1\n",
			"banner motd # one line banner\n#\nhostname sw1\n",
		},
		{"newline body", "banner motd #\n#\n", "banner motd #\n#\n"},
		{
			"nested",
			"line vty\n  banner exec ^ hi ^\n",
			"line vty\n  banner exec ^ hi ^\n",
		},
		{
			"body lines",
			"banner motd # first\nsecond\n#\n",
			"banner motd # first\nsecond\n#\n",
		},
		{
			"blank body line",
			"banner motd # first\n\n#\n",
			"banner motd # first\n\n#\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := inlineSchema()
			d := diag.New()
			cfg := parse.Parse(s, tc.in, parse.Reject, d)
			require.False(t, d.HasErrors(), d.String())
			out := Render(cfg)
			assert.Equal(t, tc.want, out)
			// The rendered form parses back to an equal tree without diagnostics.
			d2 := diag.New()
			cfg2 := parse.Parse(s, out, parse.Reject, d2)
			assert.Empty(t, d2.String())
			assert.Equal(t, out, Render(cfg2))
			assertSameTree(t, cfg.Root, cfg2.Root)
		})
	}
}

// assertSameTree compares captured fields too; SameValue stops at the rendered text.
func assertSameTree(t *testing.T, want, got *schema.Node) {
	t.Helper()
	assert.True(t, want.SameValue(got), want.Text)
	assert.Equal(t, want.Fields, got.Fields, want.Text)
	require.Len(t, got.Children, len(want.Children), want.Text)
	for i, c := range want.Children {
		assertSameTree(t, c, got.Children[i])
	}
}

func TestRenderBlockNewlineBodyKeepsBothLines(t *testing.T) {
	s := schema.New()
	s.Node("banner motd {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	in := "banner motd ^\n^\n"
	d := diag.New()
	cfg := parse.Parse(s, in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	// The body is a single newline, so both delimiters keep their own line.
	assert.Equal(t, []string{"", ""}, cfg.Root.Children[0].Block)
	assert.Equal(t, in, Render(cfg))
}

func TestRenderBlockUntilStaysMultiLine(t *testing.T) {
	s := schema.New()
	s.Node("certificate {{ name:rest }}").
		Card(schema.ZeroToN).BlockUntil("quit")
	in := "certificate ca\nquit\n"
	d := diag.New()
	cfg := parse.Parse(s, in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, in, Render(cfg))
}

// A delimiter longer than one character could not be recognized where the
// device ends the block, so the schema refuses it.
// bannerNode builds a delimited-block node directly, as a tree transform or a
// downstream package would. Parse cannot produce an empty body, so this is the
// only way to reach render's guard for it.
func bannerNode(body []string) *schema.Config {
	s := schema.New()
	def := s.Node("banner motd {{ d:delim }}").
		Card(schema.ZeroToOne).BlockDelim("d")
	cfg := schema.NewConfig(s)
	n := cfg.Root.AddChild(schema.NewNode("banner motd ^"))
	n.Def, n.Fields, n.Block = def, map[string]string{"d": "^"}, body
	return cfg
}

// A device omits the empty form, so rendering an opener and terminator would
// invent a command it never reports. validate.BlockBodies is what reports the
// node; render only has to not emit it.
func TestRenderOmitsEmptyDelimitedBlock(t *testing.T) {
	for name, body := range map[string][]string{
		"nil":            nil,
		"empty slice":    {},
		"one empty line": {""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "", Render(bannerNode(body)))
		})
	}
}

func TestRenderKeepsNonEmptyDelimitedBlock(t *testing.T) {
	assert.Equal(
		t,
		"banner motd ^ hi ^\n",
		Render(bannerNode([]string{" hi "})),
	)
	assert.Equal(
		t,
		"banner motd ^\nhi\n^\n",
		Render(bannerNode([]string{"", "hi", ""})),
	)
}
