package merge_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/merge"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

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
	iface.Child("switchport trunk allowed vlan {{ vlans:word }}").
		Card(schema.ZeroToOne).
		List("vlans", "uint").
		ListDelta("switchport trunk allowed vlan add {{ vlans }}",
			"switchport trunk allowed vlan remove {{ vlans }}")
	iface.Child("span session {{ id:word }} vlans {{ vlans:word }}").
		Card(schema.ZeroToOne).
		List("vlans", "uint")
	bgp := s.Node("router bgp {{ asn:asn }}").Card(schema.ZeroToOne)
	bgp.Child("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN)
	return s
}

func parsePart(t *testing.T, s *schema.Schema, text string) *tree.Config {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, text, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	return cfg
}

// mergeText parses each text as a part, merges them under one policy, and renders the result.
func mergeText(
	t *testing.T,
	s *schema.Schema,
	strict bool,
	texts ...string,
) (string, *diag.Diagnostics) {
	t.Helper()
	parts := make([]*tree.Config, len(texts))
	for i, txt := range texts {
		parts[i] = parsePart(t, s, txt)
	}
	out, d := merge.Merge(s, diag.Policy{Strict: strict}, parts...)
	return render.Render(out), d
}

func TestMergeDisjointParts(t *testing.T) {
	got, d := mergeText(t, testSchema(), true,
		"vlan 10\n",
		"interface eth1\n  description uplink\n")
	require.False(t, d.HasErrors())
	assert.Equal(t, "vlan 10\ninterface eth1\n  description uplink\n", got)
}

func TestMergeSameSectionInterleaves(t *testing.T) {
	got, d := mergeText(t, testSchema(), true,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  switchport access vlan 10\nvlan 10\n")
	require.False(t, d.HasErrors())
	assert.Equal(
		t,
		"interface eth1\n  description uplink\n  switchport access vlan 10\nvlan 10\n",
		got,
	)
}

func TestMergeDedupsIdenticalLines(t *testing.T) {
	got, d := mergeText(
		t,
		testSchema(),
		true,
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\n",
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\n  neighbor 10.0.0.2 remote-as 65002\n",
	)
	require.False(t, d.HasErrors())
	assert.Equal(
		t,
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\n  neighbor 10.0.0.2 remote-as 65002\n",
		got,
	)
}

func TestMergeConflictStrict(t *testing.T) {
	got, d := mergeText(t, testSchema(), true,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.True(t, d.HasErrors())
	// earlier value stays
	assert.Contains(t, got, "description uplink")
	assert.Contains(t, d.String(), "part 2")
	assert.Contains(t, d.String(), "part 1")
}

func TestMergeConflictLenientLastWins(t *testing.T) {
	got, d := mergeText(t, testSchema(), false,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.False(t, d.HasErrors())
	assert.NotEmpty(t, d.Items) // warning
	assert.Contains(t, got, "description downlink")
	assert.NotContains(t, got, "uplink")
}

func TestMergeKeyedLeafConflict(t *testing.T) {
	s := schema.New()
	s.Node("max-paths {{ mode:word }} {{ n:uint }}").
		Card(schema.ZeroToN).
		Key("mode")
	a := parsePart(t, s, "max-paths ebgp 4\n")
	b := parsePart(t, s, "max-paths ebgp 8\n")
	_, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	assert.True(t, d.HasErrors())
	out, d2 := merge.Merge(s, diag.Policy{Strict: false}, a, b)
	assert.False(t, d2.HasErrors())
	assert.Equal(t, "max-paths ebgp 8\n", render.Render(out))
}

func TestMergeNeverMutatesInputs(t *testing.T) {
	s := testSchema()
	a := parsePart(t, s, "interface eth1\n  description uplink\n")
	b := parsePart(t, s, "interface eth1\n  description downlink\n")
	beforeA, beforeB := render.Render(a), render.Render(b)
	merge.Merge(s, diag.Policy{Strict: false}, a, b)
	assert.Equal(t, beforeA, render.Render(a))
	assert.Equal(t, beforeB, render.Render(b))
}

func TestMergeSchemaMismatch(t *testing.T) {
	s1, s2 := testSchema(), testSchema()
	_, d := merge.Merge(
		s1,
		diag.Policy{Strict: true},
		parsePart(t, s2, "vlan 10\n"),
	)
	assert.True(t, d.HasErrors())
}

func TestMergeZeroAndOneParts(t *testing.T) {
	s := testSchema()
	out, d := merge.Merge(s, diag.Policy{Strict: true})
	assert.False(t, d.HasErrors())
	assert.Empty(t, out.Root.Children)

	one, d1 := mergeText(t, s, true, "vlan 10\n")
	assert.False(t, d1.HasErrors())
	assert.Equal(t, "vlan 10\n", one)
}

func TestMergeZeroToOneSlotConflict(t *testing.T) {
	// Report a conflict when two parts assign different values to one non-idempotent ZeroToOne slot.
	s := testSchema()
	a := parsePart(
		t,
		s,
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\n",
	)
	b := parsePart(
		t,
		s,
		"router bgp 65001\n  neighbor 10.0.0.2 remote-as 65002\n",
	)
	_, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	assert.True(t, d.HasErrors())

	out, d2 := merge.Merge(s, diag.Policy{Strict: false}, a, b)
	assert.False(t, d2.HasErrors())
	rendered := render.Render(out)
	// last part's header wins; children from both parts merge under it
	assert.Contains(t, rendered, "router bgp 65001")
	assert.NotContains(t, rendered, "router bgp 65000")
	assert.Contains(t, rendered, "neighbor 10.0.0.1 remote-as 65001")
	assert.Contains(t, rendered, "neighbor 10.0.0.2 remote-as 65002")
}

func TestMergeStrictSectionConflictKeepsChildren(t *testing.T) {
	// A strict header conflict keeps the first header and merges children from both parts.
	s := testSchema()
	a := parsePart(
		t,
		s,
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\n",
	)
	b := parsePart(
		t,
		s,
		"router bgp 65001\n  neighbor 10.0.0.2 remote-as 65002\n",
	)
	out, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	assert.True(t, d.HasErrors())
	rendered := render.Render(out)
	assert.Contains(t, rendered, "router bgp 65000") // first header stays
	assert.NotContains(t, rendered, "router bgp 65001")
	assert.Contains(t, rendered, "neighbor 10.0.0.1 remote-as 65001")
	assert.Contains(t, rendered, "neighbor 10.0.0.2 remote-as 65002")
}

func TestMergeCrossDefKeyedConflict(t *testing.T) {
	// The same (Kind, key) slot set via sibling templates must collide.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id").MarkIdempotent()
	s.Node("vlan {{ id:vlan }} name {{ name:word }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id").MarkIdempotent()
	a := parsePart(t, s, "vlan 10 state enable\n")
	b := parsePart(t, s, "vlan 10 name FOO state disable\n")
	_, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	assert.True(t, d.HasErrors())

	out, d2 := merge.Merge(s, diag.Policy{Strict: false}, a, b)
	assert.False(t, d2.HasErrors())
	assert.Equal(t, "vlan 10 name FOO state disable\n", render.Render(out))
}

func TestMergeCrossDefSectionsDoNotMixChildren(t *testing.T) {
	s := schema.New()
	a := s.Node("mode {{ id:word }} a").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	a.Child("a-child").Card(schema.ZeroToOne)
	b := s.Node("mode {{ id:word }} b").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	b.Child("b-child").Card(schema.ZeroToOne)
	first := parsePart(t, s, "mode 1 a\n  a-child\n")
	second := parsePart(t, s, "mode 1 b\n  b-child\n")

	strictOut, strictDiag := merge.Merge(
		s,
		diag.Policy{Strict: true},
		first,
		second,
	)
	assert.True(t, strictDiag.HasErrors())
	assert.Equal(t, "mode 1 a\n  a-child\n", render.Render(strictOut))

	out, d := merge.Merge(s, diag.Policy{Strict: false}, first, second)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "mode 1 b\n  b-child\n", render.Render(out))
	reparsed := parsePart(t, s, render.Render(out))
	assert.Equal(t, render.Render(out), render.Render(reparsed))
}

func toggleSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	shut := iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("no shutdown").Card(schema.ZeroToOne).Toggles(shut)
	return s
}

func TestMergeToggleConflictStrict(t *testing.T) {
	s := toggleSchema()
	a := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  no shutdown\n")
	out, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "part 2 conflicts with part 1")
	rendered := render.Render(out)
	assert.Contains(t, rendered, "shutdown")       // first value stays
	assert.NotContains(t, rendered, "no shutdown") // later part rejected
}

func TestMergeToggleConflictLenientLastWins(t *testing.T) {
	s := toggleSchema()
	a := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  no shutdown\n")
	out, d := merge.Merge(s, diag.Policy{Strict: false}, a, b)
	assert.False(t, d.HasErrors())
	assert.Contains(t, d.String(), "part 2 overrides part 1")
	rendered := render.Render(out)
	assert.Contains(t, rendered, "no shutdown")
	assert.Equal(t, 1, strings.Count(rendered, "shutdown")) // only the winner
}

func TestMergeToggleGroupThreeWayConflict(t *testing.T) {
	// All members of an N-way group share one slot identity: two parts
	// naming different members of a 3-way group collide like a pair does.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	auto := iface.Child("duplex auto").Card(schema.ZeroToOne)
	full := iface.Child("duplex full").Card(schema.ZeroToOne)
	iface.Child("duplex half").Card(schema.ZeroToOne).Toggles(auto, full)

	a := parsePart(t, s, "interface Ethernet1/1\n  duplex auto\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  duplex half\n")
	_, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "part 2 conflicts with part 1")
}

func TestMergeToggleSameValueDedups(t *testing.T) {
	// Deduplicate the same toggle member without a conflict or diagnostic.
	s := toggleSchema()
	a := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	out, d := merge.Merge(s, diag.Policy{Strict: true}, a, b)
	require.False(t, d.HasErrors(), d.String())
	rendered := render.Render(out)
	assert.Equal(t, 1, strings.Count(rendered, "shutdown"))
}

func TestMergeListUnion(t *testing.T) {
	got, d := mergeText(t, testSchema(), true,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		"interface eth1\n  switchport trunk allowed vlan 20,30-31\n")
	// Union sets without conflict in both policies; the composed value exists in
	// no part, so the line is the canonical render of the union.
	require.False(t, d.HasErrors(), d.String())
	assert.Empty(t, d.Items, "union must produce no diagnostics")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 10,20,30-31\n",
		got)
}

func TestMergeListUnionIdentical(t *testing.T) {
	// Identical raw text short-circuits as today's dedup, before union.
	got, d := mergeText(t, testSchema(), true,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		"interface eth1\n  switchport trunk allowed vlan 10,20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		got)
}

func TestMergeListOtherArgDiffersConflicts(t *testing.T) {
	// Non-list fields differ: the established conflict path, not a union.
	_, d := mergeText(t, testSchema(), true,
		"interface eth1\n  span session 1 vlans 10\n",
		"interface eth1\n  span session 2 vlans 20\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "conflicts with")
}

func TestMergeListMalformedFallsBackToConflict(t *testing.T) {
	// A raw value that does not Expand cannot union; lenient keeps last-wins.
	got, d := mergeText(t, testSchema(), false,
		"interface eth1\n  switchport trunk allowed vlan 30-20\n",
		"interface eth1\n  switchport trunk allowed vlan 10\n")
	require.False(t, d.HasErrors(), d.String())
	assert.NotEmpty(t, d.Items, "expected the override warning")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 10\n",
		got)
}

func kindSlotSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.ZeroToOne).Kind("default-originate")
	r.Child("default-originate").
		Card(schema.ZeroToOne).Kind("default-originate")
	return s
}

func TestMergeKindSlotLenientLastWins(t *testing.T) {
	got, d := mergeText(t, kindSlotSchema(), false,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"router bgp 65000\n  default-originate route-map RM\n", got)
}

func TestMergeKindSlotStrictConflicts(t *testing.T) {
	got, d := mergeText(t, kindSlotSchema(), true,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.True(t, d.HasErrors())
	assert.Equal(t, "router bgp 65000\n  default-originate\n", got)
	assert.Contains(t, d.String(), "conflicts")
	assert.Contains(t, d.String(), "default-originate route-map RM")
}

func TestMergeKindSlotIdenticalSpellingIsSilent(t *testing.T) {
	for _, strict := range []bool{true, false} {
		got, d := mergeText(t, kindSlotSchema(), strict,
			"router bgp 65000\n  default-originate\n",
			"router bgp 65000\n  default-originate\n")
		assert.Equal(t, "router bgp 65000\n  default-originate\n", got)
		assert.Empty(t, d.String(), "strict=%v", strict)
	}
}

func TestMergeToggleWithKindStillSharesOneSlot(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	en := i.Child("logging enable").Card(schema.ZeroToOne).Kind("log")
	i.Child("logging disable").Card(schema.ZeroToOne).Kind("log").Toggles(en)
	got, d := mergeText(t, s, false,
		"interface Ethernet1/1\n  logging enable\n",
		"interface Ethernet1/1\n  logging disable\n")
	assert.Equal(t, "interface Ethernet1/1\n  logging disable\n", got)
	assert.Contains(t, d.String(), "overrides")
}

func TestMergeKindSlotLenientWarnsOnOverride(t *testing.T) {
	_, d := mergeText(t, kindSlotSchema(), false,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.Contains(t, d.String(), "overrides")
}
