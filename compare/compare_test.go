package compare

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/schema"
)

func mustParse(t *testing.T, s *schema.Schema, text string) *schema.Config {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, text, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	return cfg
}

func TestRenderEmpty(t *testing.T) {
	assert.Equal(t, "", Render(nil))
}

func TestRenderSignsAndContext(t *testing.T) {
	iface := []string{"interface Ethernet1/1"}
	changes := []remediate.Change{
		{
			Action: graph.Modify, Running: schema.NewNode("description OLD"),
			Intended: schema.NewNode("description NEW"), Path: iface,
		},
		{
			Action:  graph.Remove,
			Running: schema.NewNode("shutdown"),
			Path:    iface,
		},
		{Action: graph.Add, Intended: schema.NewNode("vlan 99")},
	}
	want := "  interface Ethernet1/1\n" +
		"-   description OLD\n" +
		"+   description NEW\n" +
		"-   shutdown\n" +
		"+ vlan 99\n"
	assert.Equal(t, want, Render(changes))
}

func TestRenderRegroupsSplitSections(t *testing.T) {
	e11 := []string{"interface Ethernet1/1"}
	e12 := []string{"interface Ethernet1/2"}
	changes := []remediate.Change{
		{
			Action:   graph.Add,
			Intended: schema.NewNode("description A"),
			Path:     e11,
		},
		{Action: graph.Add, Intended: schema.NewNode("mtu 9000"), Path: e12},
		{Action: graph.Add, Intended: schema.NewNode("no shutdown"), Path: e11},
	}
	want := "  interface Ethernet1/1\n" +
		"+   description A\n" +
		"+   no shutdown\n" +
		"  interface Ethernet1/2\n" +
		"+   mtu 9000\n"
	assert.Equal(t, want, Render(changes))
}

func TestRenderSharedAncestorPrefix(t *testing.T) {
	bgp := []string{"router bgp 65000"}
	af := []string{"router bgp 65000", "address-family ipv4 unicast"}
	changes := []remediate.Change{
		{
			Action:   graph.Add,
			Intended: schema.NewNode("router-id 1.1.1.1"),
			Path:     bgp,
		},
		{
			Action:   graph.Add,
			Intended: schema.NewNode("max-paths ebgp 8"),
			Path:     af,
		},
	}
	want := "  router bgp 65000\n" +
		"+   router-id 1.1.1.1\n" +
		"    address-family ipv4 unicast\n" +
		"+     max-paths ebgp 8\n"
	assert.Equal(t, want, Render(changes))
}

func TestRenderRemovedSectionShowsSubtree(t *testing.T) {
	sec := schema.NewNode("interface Ethernet1/1")
	sec.AddChild(schema.NewNode("description A"))
	sec.AddChild(schema.NewNode("shutdown"))
	changes := []remediate.Change{{Action: graph.Remove, Running: sec}}
	want := "- interface Ethernet1/1\n" +
		"-   description A\n" +
		"-   shutdown\n"
	assert.Equal(t, want, Render(changes))
}

func TestRenderToggleFlipPairFromDiff(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	shut := iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("no shutdown").Card(schema.ZeroToOne).Toggles(shut)
	res, d := remediate.Diff(
		mustParse(t, s, "interface Ethernet1/1\n  shutdown\n"),
		mustParse(t, s, "interface Ethernet1/1\n  no shutdown\n"),
		remediate.Options{Cycle: remediate.Break},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "  interface Ethernet1/1\n" +
		"-   shutdown\n" +
		"+   no shutdown\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderToggleFlipShowsBothSubtrees(t *testing.T) {
	s := schema.New()
	on := s.Node("feature on").Card(schema.ZeroToOne)
	on.Child("old child").Card(schema.ZeroToOne)
	off := s.Node("feature off").Card(schema.ZeroToOne)
	off.Child("new child").Card(schema.ZeroToOne)
	off.Toggles(on)
	res, d := remediate.Diff(
		mustParse(t, s, "feature on\n  old child\n"),
		mustParse(t, s, "feature off\n  new child\n"),
		remediate.Options{Cycle: remediate.Abort},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- feature on\n" +
		"-   old child\n" +
		"+ feature off\n" +
		"+   new child\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderRemovedSectionFromDiff(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	iface.Child("shutdown").Card(schema.ZeroToOne)
	res, d := remediate.Diff(
		mustParse(t, s, "interface Ethernet1/1\n  description A\n  shutdown\n"),
		mustParse(t, s, ""),
		remediate.Options{Cycle: remediate.Break},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- interface Ethernet1/1\n" +
		"-   description A\n" +
		"-   shutdown\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderSectionHeaderModifyHeaderOnly(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	vlan := s.Node("vlan {{ id:vlan }} name {{ name:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	vlan.Child("shutdown").Card(schema.ZeroToOne)
	res, d := remediate.Diff(
		mustParse(t, s, "vlan 10 name FOO\n  shutdown\n"),
		mustParse(t, s, "vlan 10 name BAR\n"),
		remediate.Options{Cycle: remediate.Break},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- vlan 10 name FOO\n" +
		"+ vlan 10 name BAR\n" +
		"  vlan 10 name BAR\n" +
		"-   shutdown\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderReplacePairFromDiff(t *testing.T) {
	// Render Replace as one removal and addition pair.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }} state {{ st:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	res, d := remediate.Diff(
		mustParse(t, s, "vlan 10 state enable\n"),
		mustParse(t, s, "vlan 10 state disable\n"),
		remediate.Options{Cycle: remediate.Break},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- vlan 10 state enable\n" +
		"+ vlan 10 state disable\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderNoSectionExitToken(t *testing.T) {
	// The view shows config state, not paste-ready CLI: no exit token.
	s := schema.New()
	testtypes.Fill(s.Registry)
	bgp := s.Node("router bgp {{ asn:asn }}").Card(schema.ZeroToOne)
	af := bgp.Child("address-family {{ af:word }} unicast").
		Card(schema.ZeroToN).SectionExit("exit-address-family")
	af.Child("max-paths ebgp {{ n:uint }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	res, d := remediate.Diff(
		mustParse(t, s, ""),
		mustParse(t, s, "router bgp 65000\n"+
			"  address-family ipv4 unicast\n"+
			"    max-paths ebgp 8\n"),
		remediate.Options{Cycle: remediate.Break},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "+ router bgp 65000\n" +
		"+   address-family ipv4 unicast\n" +
		"+     max-paths ebgp 8\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderBlockBody(t *testing.T) {
	s := schema.New()
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().BlockDelim("delim")
	res, d := remediate.Diff(
		mustParse(t, s, "banner motd ^\nhello\n^\n"),
		mustParse(t, s, "banner motd ^\nworld\n^\n"),
		remediate.Options{Cycle: remediate.Break},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- banner motd ^\n" +
		"- hello\n" +
		"- ^\n" +
		"+ banner motd ^\n" +
		"+ world\n" +
		"+ ^\n"
	assert.Equal(t, want, Render(res.Changes))
}
