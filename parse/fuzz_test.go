package parse

import (
	"testing"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// FuzzParse asserts the indent-stack walk never panics for arbitrary input
// (any indentation, unknown commands, truncation) under both policies, and
// always returns a non-nil config. Run the seed corpus via `go test`; explore
// with `go test -run x -fuzz FuzzParse ./parse/`.
func FuzzParse(f *testing.F) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}").
		Card(schema.ZeroToOne)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("banner motd {{ delim:word }}").
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
			// Every node has a panic-free Path and normalized text.
			tree.Walk(cfg, func(n *tree.Node) {
				_ = n.Path()
				if got := normalize(n.Text); got != n.Text {
					t.Fatalf(
						"node text not normalized: %q != %q",
						n.Text,
						got,
					)
				}
			})
			// Render is panic-free and deterministic.
			first := render.Render(cfg)
			if second := render.Render(cfg); first != second {
				t.Fatalf("Render not deterministic for %q", in)
			}
		}
	})
}
