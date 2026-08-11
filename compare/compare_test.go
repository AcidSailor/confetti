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
	"github.com/acidsailor/confetti/tree"
)

func mustParse(t *testing.T, s *schema.Schema, text string) *tree.Config {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, text, diag.Policy{Strict: true}, d)
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
			Action: graph.Modify, Running: tree.NewNode("description OLD"),
			Intended: tree.NewNode("description NEW"), Path: iface,
		},
		{Action: graph.Remove, Running: tree.NewNode("shutdown"), Path: iface},
		{Action: graph.Add, Intended: tree.NewNode("vlan 99")},
	}
	want := "  interface Ethernet1/1\n" +
		"-   description OLD\n" +
		"+   description NEW\n" +
		"-   shutdown\n" +
		"+ vlan 99\n"
	assert.Equal(t, want, Render(changes))
}

func TestRenderRegroupsSplitSections(t *testing.T) {
	// Scheduled order interleaves two sections; the view reassembles each
	// hunk by path on first appearance.
	e11 := []string{"interface Ethernet1/1"}
	e12 := []string{"interface Ethernet1/2"}
	changes := []remediate.Change{
		{Action: graph.Add, Intended: tree.NewNode("description A"), Path: e11},
		{Action: graph.Add, Intended: tree.NewNode("mtu 9000"), Path: e12},
		{Action: graph.Add, Intended: tree.NewNode("no shutdown"), Path: e11},
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
			Intended: tree.NewNode("router-id 1.1.1.1"),
			Path:     bgp,
		},
		{
			Action:   graph.Add,
			Intended: tree.NewNode("max-paths ebgp 8"),
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
	// The artifact negates only the header; the view shows every
	// disappearing line through the paired running-side node.
	sec := tree.NewNode("interface Ethernet1/1")
	sec.AddChild(tree.NewNode("description A"))
	sec.AddChild(tree.NewNode("shutdown"))
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
		diag.Policy{},
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
		diag.Policy{Strict: true},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- feature on\n" +
		"-   old child\n" +
		"+ feature off\n" +
		"+   new child\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderRemovedSectionFromDiff(t *testing.T) {
	// Closes the composition gap left by TestRenderRemovedSectionShowsSubtree
	// (hand-built subtree): whole-section removal driven end-to-end through
	// Diff, confirming Change.Running carries the full running subtree even
	// though the artifact negates only the header.
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
		diag.Policy{},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "- interface Ethernet1/1\n" +
		"-   description A\n" +
		"-   shutdown\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderSectionHeaderModifyHeaderOnly(t *testing.T) {
	// A paired section whose header carries a changed non-key field renders
	// as a header-only -/+ pair; the changed child appears once, as its own
	// Change, not repeated under both subtree signs.
	s := schema.New()
	testtypes.Fill(s.Registry)
	vlan := s.Node("vlan {{ id:vlan }} name {{ name:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	vlan.Child("shutdown").Card(schema.ZeroToOne)
	res, d := remediate.Diff(
		mustParse(t, s, "vlan 10 name FOO\n  shutdown\n"),
		mustParse(t, s, "vlan 10 name BAR\n"),
		diag.Policy{},
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
		diag.Policy{},
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
		diag.Policy{},
	)
	require.False(t, d.HasErrors(), d.String())
	want := "+ router bgp 65000\n" +
		"+   address-family ipv4 unicast\n" +
		"+     max-paths ebgp 8\n"
	assert.Equal(t, want, Render(res.Changes))
}

func TestRenderBlockBody(t *testing.T) {
	// Body change on an idempotent block slot is a Modify: both blocks shown,
	// bodies verbatim (never indented), terminator included.
	s := schema.New()
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().BlockDelim("delim")
	res, d := remediate.Diff(
		mustParse(t, s, "banner motd ^\nhello\n^\n"),
		mustParse(t, s, "banner motd ^\nworld\n^\n"),
		diag.Policy{},
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
