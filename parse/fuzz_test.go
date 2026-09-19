package parse

import (
	"maps"
	"slices"
	"testing"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

// FuzzParse checks parser safety, text normalization, and render determinism and idempotence.
func FuzzParse(f *testing.F) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}").
		Card(schema.ZeroToOne)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("banner motd {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	// The trailing capture permits inline close.
	s.Node("banner login {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	// A nested block takes its candidates from the parent definition.
	vty := s.Node("line {{ name:word }}").Card(schema.ZeroToN)
	vty.Child("banner exec {{ delim:delim }}").
		Card(schema.ZeroToOne).BlockDelim("delim")

	seeds := []string{
		"",
		"\n\n\n",
		"interface Ethernet1/1\n  shutdown\n",
		"   \t interface X\n\t\tip address 1.1.1.1 2.2.2.2\n",
		"vlan 10\nvlan 20\nbogus block\n  deeper unknown\n",
		"  \t  \n  leading indent with no parent\n",
		"banner motd ^\nhello\n^\n",
		"banner motd ^\nunterminated\n",
		"banner motd ^\n^\n",
		"banner motd ^\n\n  ! weird\n^\n",
		"banner login ^ hi ^\n",
		"banner login ^^\n",
		"banner login ^ a ^ b ^\n",
		"banner login ^ hi ^ ^\n",
		"banner login ^\nhi\n^\n",
		"line vty\n  banner exec ^ hi ^\n",
		"line vty\n  banner exec ^ hi\n^\n",
		"line vty\n  banner exec ^\n^\n",
		"line vty\n  banner exec ^ a ^ b ^\n",
	}
	for _, sd := range seeds {
		f.Add(sd)
	}

	f.Fuzz(func(t *testing.T, in string) {
		for _, unknown := range []Unknown{Reject, Drop} {
			d := diag.New()
			cfg := Parse(s, in, unknown, d)
			if cfg == nil {
				t.Fatalf("Parse returned nil config for %q", in)
			}
			schema.Walk(cfg, func(n *schema.Node) {
				_ = n.Path()
				if got := normalize(n.Text); got != n.Text {
					t.Fatalf(
						"node text not normalized: %q != %q",
						n.Text,
						got,
					)
				}
			})
			first := render.Render(cfg)
			if second := render.Render(cfg); first != second {
				t.Fatalf("Render not deterministic for %q", in)
			}
			rd := diag.New()
			back := Parse(s, first, unknown, rd)
			again := render.Render(back)
			if again != first {
				t.Fatalf(
					"render not idempotent for %q: %q then %q",
					in,
					first,
					again,
				)
			}
			// Rejected input has no canonical form, so both round-trip
			// promises apply only to input that parsed cleanly.
			if d.HasErrors() {
				continue
			}
			// Rendering a clean parse must not invent problems of its own.
			if rd.HasErrors() {
				t.Fatalf(
					"render of %q introduced errors: %s",
					in,
					rd.String(),
				)
			}
			// Idempotent text is not enough: the tree must survive too.
			sameTree(t, in, cfg.Root, back.Root)
		}
	})
}

// sameTree fails unless both subtrees carry the same definitions, text, fields, and bodies.
func sameTree(t *testing.T, in string, want, got *schema.Node) {
	t.Helper()
	if want.Def != got.Def || want.Text != got.Text {
		t.Fatalf(
			"round trip changed a node for %q: %q -> %q",
			in,
			want.Text,
			got.Text,
		)
	}
	if !maps.Equal(want.Fields, got.Fields) {
		t.Fatalf(
			"round trip changed fields of %q for %q: %v -> %v",
			want.Text,
			in,
			want.Fields,
			got.Fields,
		)
	}
	if !slices.Equal(want.Block, got.Block) {
		t.Fatalf(
			"round trip changed the body of %q for %q: %q -> %q",
			want.Text,
			in,
			want.Block,
			got.Block,
		)
	}
	if len(want.Children) != len(got.Children) {
		t.Fatalf(
			"round trip changed the children of %q for %q: %d -> %d",
			want.Text,
			in,
			len(want.Children),
			len(got.Children),
		)
	}
	for i, c := range want.Children {
		sameTree(t, in, c, got.Children[i])
	}
}
