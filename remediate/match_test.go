package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// testSchema is the shared fixture for the remediate suite: vlan (keyed, with a
// child), interface with idempotent + full-line + toggle children, and the
// vlan cross-ref.
func testSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	vlan := s.Node("vlan {{ id:vlan }}").
		Card(schema.ZeroToN).
		Kind("vlan").
		Key("id")
	vlan.Child("name {{ text:word }}").Card(schema.ZeroToOne).MarkIdempotent()
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().Ref("vlan", "vlan.id")
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }} secondary").
		Card(schema.ZeroToN)
	shut := iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("no shutdown").Card(schema.ZeroToOne).Toggles(shut)
	iface.Child("switchport trunk allowed vlan {{ vlans:word }}").
		Card(schema.ZeroToOne).
		List("vlans", "uint").
		ListDelta("switchport trunk allowed vlan add {{ vlans }}",
			"switchport trunk allowed vlan remove {{ vlans }}")
	iface.Child("monitor vlan {{ vlans:word }}").
		Card(schema.ZeroToOne).
		List("vlans", "uint") // no delta forms: whole-line modify fallback
	iface.Child("core vlans {{ vlans:word }}").
		Card(schema.ZeroToOne).
		Protect().
		List("vlans", "uint").
		ListDelta("core vlans add {{ vlans }}", "core vlans remove {{ vlans }}")
	s.Node("vlan-group {{ name:word }} vlans {{ vlans:word }}").
		Card(schema.ZeroToN).Kind("vlan-group").Key("name").
		List("vlans", "uint").
		ListDelta("vlan-group {{ name }} vlans add {{ vlans }}",
			"vlan-group {{ name }} vlans remove {{ vlans }}")
	return s
}

func mustParse(t *testing.T, s *schema.Schema, text string) *tree.Config {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, text, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	return cfg
}

// topNode returns the nth top-level node of a parsed config.
func topNode(cfg *tree.Config, i int) *tree.Node { return cfg.Root.Children[i] }

func TestCategoryOf(t *testing.T) {
	s := testSchema()
	cfg := mustParse(
		t,
		s,
		"vlan 10\ninterface Ethernet1/1\n  description X\n  shutdown\n  ip address 10.0.0.1 255.255.255.0 secondary\n",
	)
	vlan := topNode(cfg, 0)
	iface := topNode(cfg, 1)
	desc := iface.Children[0]
	shut := iface.Children[1]
	sec := iface.Children[2]

	assert.Equal(t, ident.Keyed, ident.CategoryOf(vlan))
	assert.Equal(t, ident.IdempotentSingle, ident.CategoryOf(desc))
	assert.Equal(t, ident.FullLine, ident.CategoryOf(shut))
	assert.Equal(t, ident.FullLine, ident.CategoryOf(sec))
}

func TestIdentPairsAndDistinguishes(t *testing.T) {
	s := testSchema()
	a := mustParse(t, s, "vlan 10\nvlan 20\n")
	// same keyed def, same key => equal ident
	assert.Equal(
		t,
		ident.Of(a.Root.Children[0]),
		ident.Of(mustParse(t, s, "vlan 10\n").Root.Children[0]),
	)
	// same keyed def, different key => different ident
	assert.NotEqual(
		t,
		ident.Of(a.Root.Children[0]),
		ident.Of(a.Root.Children[1]),
	)
}

func TestIdentIdempotentIgnoresValue(t *testing.T) {
	s := testSchema()
	x := mustParse(t, s, "interface Ethernet1/1\n  description A\n").Root.Children[0].Children[0]
	y := mustParse(t, s, "interface Ethernet1/1\n  description B\n").Root.Children[0].Children[0]
	// idempotent slot: same def, value excluded => identical ident => pairs => Modify
	assert.Equal(t, ident.Of(x), ident.Of(y))
}

func TestIsSection(t *testing.T) {
	s := testSchema()
	cfg := mustParse(t, s, "vlan 10\ninterface Ethernet1/1\n  shutdown\n")
	assert.True(
		t,
		ident.IsSection(cfg.Root.Children[0]),
	) // vlan has a child def (name)
	assert.True(
		t,
		ident.IsSection(cfg.Root.Children[1]),
	) // interface has child defs
	assert.False(
		t,
		ident.IsSection(cfg.Root.Children[1].Children[0]),
	) // shutdown: leaf
}

func TestIndexByIdentFirstWins(t *testing.T) {
	s := testSchema()
	cfg := mustParse(
		t,
		s,
		"vlan 10\nvlan 10\n",
	) // ImportCheck is not run here, so the duplicate reaches the index.
	idx := indexByIdent(cfg.Root.Children)
	require.Len(t, idx, 1)
	assert.Same(t, cfg.Root.Children[0], idx[ident.Of(cfg.Root.Children[0])])
	// The loser maps to the winner, which is how collect detects and reports it.
	assert.NotSame(t, cfg.Root.Children[1], idx[ident.Of(cfg.Root.Children[1])])
}
