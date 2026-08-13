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

var (
	refuse   = merge.Options{Resolve: merge.Refuse}
	declared = merge.Options{}
)

func parsePart(t *testing.T, s *schema.Schema, text string) *schema.Config {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, text, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	return cfg
}

func mergeText(
	t *testing.T,
	s *schema.Schema,
	opts merge.Options,
	texts ...string,
) (string, *diag.Diagnostics) {
	t.Helper()
	parts := make([]*schema.Config, len(texts))
	for i, txt := range texts {
		parts[i] = parsePart(t, s, txt)
	}
	out, d := merge.Merge(s, opts, parts...)
	return render.Render(out), d
}

func TestMergeDisjointParts(t *testing.T) {
	got, d := mergeText(t, testSchema(), refuse,
		"vlan 10\n",
		"interface eth1\n  description uplink\n")
	require.False(t, d.HasErrors())
	assert.Equal(t, "vlan 10\ninterface eth1\n  description uplink\n", got)
}

func TestMergeSameSectionInterleaves(t *testing.T) {
	got, d := mergeText(t, testSchema(), refuse,
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
		refuse,
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

func TestMergeConflictRefused(t *testing.T) {
	got, d := mergeText(t, testSchema(), refuse,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.True(t, d.HasErrors())
	// earlier value stays
	assert.Contains(t, got, "description uplink")
	assert.Contains(t, d.String(), "part 2")
	assert.Contains(t, d.String(), "part 1")
}

func TestMergeConflictDeclaredLastWins(t *testing.T) {
	got, d := mergeText(t, testSchema(), declared,
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
	_, d := merge.Merge(s, refuse, a, b)
	assert.True(t, d.HasErrors())
	out, d2 := merge.Merge(s, declared, a, b)
	assert.False(t, d2.HasErrors())
	assert.Equal(t, "max-paths ebgp 8\n", render.Render(out))
}

func TestMergeNeverMutatesInputs(t *testing.T) {
	s := testSchema()
	a := parsePart(t, s, "interface eth1\n  description uplink\n")
	b := parsePart(t, s, "interface eth1\n  description downlink\n")
	beforeA, beforeB := render.Render(a), render.Render(b)
	merge.Merge(s, declared, a, b)
	assert.Equal(t, beforeA, render.Render(a))
	assert.Equal(t, beforeB, render.Render(b))
}

func TestMergeSchemaMismatch(t *testing.T) {
	s1, s2 := testSchema(), testSchema()
	_, d := merge.Merge(
		s1,
		refuse,
		parsePart(t, s2, "vlan 10\n"),
	)
	assert.True(t, d.HasErrors())
}

func TestMergeZeroAndOneParts(t *testing.T) {
	s := testSchema()
	out, d := merge.Merge(s, refuse)
	assert.False(t, d.HasErrors())
	assert.Empty(t, out.Root.Children)

	one, d1 := mergeText(t, s, refuse, "vlan 10\n")
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
	_, d := merge.Merge(s, refuse, a, b)
	assert.True(t, d.HasErrors())

	out, d2 := merge.Merge(s, declared, a, b)
	assert.False(t, d2.HasErrors())
	rendered := render.Render(out)
	// last part's header wins; children from both parts merge under it
	assert.Contains(t, rendered, "router bgp 65001")
	assert.NotContains(t, rendered, "router bgp 65000")
	assert.Contains(t, rendered, "neighbor 10.0.0.1 remote-as 65001")
	assert.Contains(t, rendered, "neighbor 10.0.0.2 remote-as 65002")
}

func TestMergeRefusedSectionKeepsFirstStanzaWhole(t *testing.T) {
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
	out, d := merge.Merge(s, refuse, a, b)
	assert.True(t, d.HasErrors())
	rendered := render.Render(out)
	assert.Contains(t, rendered, "router bgp 65000") // first header stays
	assert.NotContains(t, rendered, "router bgp 65001")
	assert.Contains(t, rendered, "neighbor 10.0.0.1 remote-as 65001")
	assert.NotContains(t, rendered, "neighbor 10.0.0.2 remote-as 65002")
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
	_, d := merge.Merge(s, refuse, a, b)
	assert.True(t, d.HasErrors())

	out, d2 := merge.Merge(s, declared, a, b)
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
		refuse,
		first,
		second,
	)
	assert.True(t, strictDiag.HasErrors())
	assert.Equal(t, "mode 1 a\n  a-child\n", render.Render(strictOut))

	out, d := merge.Merge(s, declared, first, second)
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

func TestMergeToggleConflictRefused(t *testing.T) {
	s := toggleSchema()
	a := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  no shutdown\n")
	out, d := merge.Merge(s, refuse, a, b)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "part 2 conflicts with part 1")
	rendered := render.Render(out)
	assert.Contains(t, rendered, "shutdown")       // first value stays
	assert.NotContains(t, rendered, "no shutdown") // later part rejected
}

func TestMergeToggleConflictDeclaredLastWins(t *testing.T) {
	s := toggleSchema()
	a := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  no shutdown\n")
	out, d := merge.Merge(s, declared, a, b)
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
	_, d := merge.Merge(s, refuse, a, b)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "part 2 conflicts with part 1")
}

func TestMergeToggleSameValueDedups(t *testing.T) {
	// Deduplicate the same toggle member without a conflict or diagnostic.
	s := toggleSchema()
	a := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	b := parsePart(t, s, "interface Ethernet1/1\n  shutdown\n")
	out, d := merge.Merge(s, refuse, a, b)
	require.False(t, d.HasErrors(), d.String())
	rendered := render.Render(out)
	assert.Equal(t, 1, strings.Count(rendered, "shutdown"))
}

func TestMergeListUnion(t *testing.T) {
	got, d := mergeText(t, testSchema(), refuse,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		"interface eth1\n  switchport trunk allowed vlan 20,30-31\n")
	// A synthesized union reports a value-changing combination.
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, d.Items, 1)
	assert.Contains(t, d.String(), "combines with")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 10,20,30-31\n",
		got)
}

func TestMergeListUnionIdentical(t *testing.T) {
	// Identical raw text short-circuits as today's dedup, before union.
	got, d := mergeText(t, testSchema(), refuse,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		"interface eth1\n  switchport trunk allowed vlan 10,20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		got)
}

func TestMergeListOtherArgDiffersConflicts(t *testing.T) {
	// Non-list fields differ: the established conflict path, not a union.
	_, d := mergeText(t, testSchema(), refuse,
		"interface eth1\n  span session 1 vlans 10\n",
		"interface eth1\n  span session 2 vlans 20\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "conflicts with")
}

func TestMergeListMalformedFallsBackToConflict(t *testing.T) {
	// A raw value that does not Expand cannot union; lenient keeps last-wins.
	got, d := mergeText(t, testSchema(), declared,
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

func TestMergeKindSlotDeclaredLastWins(t *testing.T) {
	got, d := mergeText(t, kindSlotSchema(), declared,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"router bgp 65000\n  default-originate route-map RM\n", got)
}

func TestMergeKindSlotRefusedConflicts(t *testing.T) {
	got, d := mergeText(t, kindSlotSchema(), refuse,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.True(t, d.HasErrors())
	assert.Equal(t, "router bgp 65000\n  default-originate\n", got)
	assert.Contains(t, d.String(), "conflicts")
	assert.Contains(t, d.String(), "default-originate route-map RM")
}

func TestMergeKindSlotIdenticalSpellingIsSilent(t *testing.T) {
	for _, opts := range []merge.Options{refuse, declared} {
		got, d := mergeText(t, kindSlotSchema(), opts,
			"router bgp 65000\n  default-originate\n",
			"router bgp 65000\n  default-originate\n")
		assert.Equal(t, "router bgp 65000\n  default-originate\n", got)
		assert.Empty(t, d.String())
	}
}

func TestMergeToggleWithKindStillSharesOneSlot(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	en := i.Child("logging enable").Card(schema.ZeroToOne).Kind("log")
	i.Child("logging disable").Card(schema.ZeroToOne).Kind("log").Toggles(en)
	got, d := mergeText(t, s, declared,
		"interface Ethernet1/1\n  logging enable\n",
		"interface Ethernet1/1\n  logging disable\n")
	assert.Equal(t, "interface Ethernet1/1\n  logging disable\n", got)
	assert.Contains(t, d.String(), "overrides")
}

func TestMergeKindSlotDeclaredWarnsOnOverride(t *testing.T) {
	_, d := mergeText(t, kindSlotSchema(), declared,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.Contains(t, d.String(), "overrides")
}

func keepSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("hostname {{ name:word }}").Card(schema.ZeroToOne).MergeKeepLast()
	s.Node("logging host {{ ip:ipv4 }}").Card(schema.ZeroToOne).MergeKeepFirst()
	return s
}

func TestMergeDeclaredKindsResolveWithoutConflict(t *testing.T) {
	for _, opts := range []merge.Options{refuse, declared} {
		got, d := mergeText(t, keepSchema(), opts,
			"hostname sw1\nlogging host 10.0.0.1\n",
			"hostname sw2\nlogging host 10.0.0.2\n")
		require.False(t, d.HasErrors(), d.String())
		assert.Contains(t, got, "hostname sw2")
		assert.Contains(t, got, "logging host 10.0.0.1")
		assert.Contains(t, d.String(), "overrides")
	}
}

func TestMergeKeepLastSectionOwnsWholeStanza(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ asn:asn }}").
		Card(schema.ZeroToOne).
		MergeKeepLast()
	r.Child("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN)
	got, d := mergeText(t, s, declared,
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\n",
		"router bgp 65000\n  neighbor 10.0.0.2 remote-as 65002\n")
	require.False(t, d.HasErrors(), d.String())
	assert.NotContains(t, got, "neighbor 10.0.0.1")
	assert.Contains(t, got, "neighbor 10.0.0.2")
	assert.Contains(t, d.String(), "part 2 overrides part 1")
}

func TestMergeCallerResolverOverridesDeclaredKind(t *testing.T) {
	keepFirst := func(
		earlier, _ *schema.Node,
		_ schema.MergeStrategy,
	) (*schema.Node, schema.Outcome) {
		return earlier, schema.Overridden
	}
	got, d := mergeText(
		t,
		keepSchema(),
		merge.Options{Resolve: keepFirst},
		"hostname sw1\n",
		"hostname sw2\n",
	)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "hostname sw1\n", got)
	assert.Contains(t, d.String(), "part 1 overrides part 2")
}

func TestMergeCallerKeepLastResolvesUnknownConflicts(t *testing.T) {
	keepLast := func(
		_, later *schema.Node,
		_ schema.MergeStrategy,
	) (*schema.Node, schema.Outcome) {
		return later, schema.Overridden
	}
	got, d := mergeText(t, testSchema(), merge.Options{Resolve: keepLast},
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, got, "description downlink")
	assert.NotContains(t, got, "uplink")
}

func TestMergeResolverTextMustRenderFromFields(t *testing.T) {
	bad := func(earlier, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		n := earlier.CloneValue()
		n.Fields["text"] = "patched"
		return n, schema.Overridden
	}
	got, d := mergeText(t, testSchema(), merge.Options{Resolve: bad},
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not render from its fields")
	assert.Contains(t, got, "description uplink")
}

func TestMergeUnionListsRejectsNonListSlots(t *testing.T) {
	s := testSchema()
	a := parsePart(t, s, "interface eth1\n  description uplink\n")
	b := parsePart(t, s, "interface eth1\n  description downlink\n")
	_, ok := merge.UnionLists(
		a.Root.Children[0].Children[0],
		b.Root.Children[0].Children[0],
	)
	assert.False(t, ok)
}

func TestMergeUnionSubsetIsSilent(t *testing.T) {
	got, d := mergeText(t, testSchema(), declared,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n",
		"interface eth1\n  switchport trunk allowed vlan 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Empty(t, d.Items)
	assert.Contains(t, got, "switchport trunk allowed vlan 10,20")
}

func TestMergeResolverTextMustBindOwnDefinition(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	i.Child("switchport mode trunk").Card(schema.ZeroToOne).Kind("mode")
	gen := i.Child("switchport mode {{ m:word }}").
		Card(schema.ZeroToOne).Kind("mode")
	bad := func(_, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		n := schema.NewNode("")
		n.Def = gen
		n.Fields = map[string]string{"m": "trunk"}
		n.Text = gen.Render(n.Fields)
		return n, schema.Overridden
	}
	got, d := mergeText(t, s, merge.Options{Resolve: bad},
		"interface eth1\n  switchport mode access\n",
		"interface eth1\n  switchport mode trunk\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not bind its own definition")
	assert.Contains(t, got, "switchport mode access")
}

func TestMergeCombinedAcrossDefinitionsIsAnError(t *testing.T) {
	s := schema.New()
	a := s.Node("mode {{ id:word }} a").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	a.Child("a-child").Card(schema.ZeroToOne)
	b := s.Node("mode {{ id:word }} b").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	b.Child("b-child").Card(schema.ZeroToOne)
	combine := func(_, later *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		return later, schema.Combined
	}
	got, d := mergeText(t, s, merge.Options{Resolve: combine},
		"mode 1 a\n  a-child\n",
		"mode 1 b\n  b-child\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "different definitions")
	assert.Equal(t, "mode 1 a\n  a-child\n", got)
}

func TestMergeCombinedOriginTracksLastContributor(t *testing.T) {
	_, d := mergeText(t, testSchema(), declared,
		"router bgp 65000\n",
		"router bgp 65001\n",
		"router bgp 65002\n")
	assert.Contains(t, d.String(),
		`part 2 combines with part 1 (was "router bgp 65000")`)
	assert.Contains(t, d.String(),
		`part 3 combines with part 2 (was "router bgp 65001")`)
}

func TestMergeFuncCustomCombination(t *testing.T) {
	build := func() *schema.Schema {
		s := schema.New()
		testtypes.Fill(s.Registry)
		i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
		desc := i.Child("description {{ text:rest }}").Card(schema.ZeroToOne).
			MarkIdempotent()
		desc.MergeFunc(
			func(earlier, later *schema.Node) (*schema.Node, schema.Outcome) {
				n := earlier.CloneValue()
				n.Fields["text"] = earlier.Fields["text"] + " + " + later.Fields["text"]
				n.Text = desc.Render(n.Fields)
				return n, schema.Combined
			},
		)
		return s
	}
	for _, opts := range []merge.Options{declared, refuse} {
		got, d := mergeText(t, build(), opts,
			"interface eth1\n  description uplink\n",
			"interface eth1\n  description downlink\n")
		require.False(t, d.HasErrors(), d.String())
		assert.Contains(t, got, "description uplink + downlink")
		assert.Contains(t, d.String(), "combines with")
	}
}

func TestCallerResolverReachesDeclaredMergeFunc(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	desc := i.Child("description {{ text:rest }}").Card(schema.ZeroToOne).
		MarkIdempotent()
	desc.MergeFunc(
		func(earlier, later *schema.Node) (*schema.Node, schema.Outcome) {
			n := earlier.CloneValue()
			n.Fields["text"] = earlier.Fields["text"] + " + " + later.Fields["text"]
			n.Text = desc.Render(n.Fields)
			return n, schema.Combined
		},
	)
	delegate := func(
		earlier, later *schema.Node,
		declared schema.MergeStrategy,
	) (*schema.Node, schema.Outcome) {
		if declared.Func != nil {
			return declared.Func(earlier, later)
		}
		return earlier, schema.Overridden
	}
	got, d := mergeText(t, s, merge.Options{Resolve: delegate},
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, got, "description uplink + downlink")
}

func TestMergeFuncResultIsValidated(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	i.Child("description {{ text:rest }}").Card(schema.ZeroToOne).
		MergeFunc(func(earlier, _ *schema.Node) (*schema.Node, schema.Outcome) {
			n := earlier.CloneValue()
			n.Fields["text"] = "patched"
			return n, schema.Overridden // Text no longer matches fields.
		})
	got, d := mergeText(t, s, declared,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not render from its fields")
	assert.Contains(t, got, "description uplink")
}

func TestMergeFuncRefusesExplicitly(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	i.Child("description {{ text:rest }}").Card(schema.ZeroToOne).
		MergeFunc(func(_, _ *schema.Node) (*schema.Node, schema.Outcome) {
			return nil, schema.Refused
		})
	got, d := mergeText(t, s, declared,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "conflicts with")
	assert.Contains(t, got, "description uplink")
}

func TestMergeCombinedKeepingEarlierAcrossDefsIsAnError(t *testing.T) {
	s := schema.New()
	a := s.Node("mode {{ id:word }} a").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	a.Child("a-child").Card(schema.ZeroToOne)
	b := s.Node("mode {{ id:word }} b").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	b.Child("b-child").Card(schema.ZeroToOne)
	combine := func(earlier, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		return earlier, schema.Combined
	}
	got, d := mergeText(t, s, merge.Options{Resolve: combine},
		"mode 1 a\n  a-child\n",
		"mode 1 b\n  b-child\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "different definitions")
	assert.Equal(t, "mode 1 a\n  a-child\n", got)
}

func TestMergeResolverNodeWithoutDefinitionIsAnError(t *testing.T) {
	bad := func(_, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		return schema.NewNode("description merged"), schema.Overridden
	}
	got, d := mergeText(t, testSchema(), merge.Options{Resolve: bad},
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "without a definition")
	assert.Contains(t, got, "description uplink")
}

func TestMergeResolverMustKeepSlotIdentity(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:uint }} name {{ name:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	rekey := func(earlier, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		n := earlier.CloneValue()
		n.Fields = map[string]string{"id": "20", "name": "x"}
		n.Text = n.Def.Render(n.Fields)
		return n, schema.Overridden
	}
	got, d := mergeText(t, s, merge.Options{Resolve: rekey},
		"vlan 10 name a\n",
		"vlan 10 name b\nvlan 20 name d\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "changed the slot's identity")
	assert.Equal(t, "vlan 10 name a\nvlan 20 name d\n", got)
}

func TestMergeResolverNodeAlreadyInTreeIsAnError(t *testing.T) {
	for _, out := range []schema.Outcome{schema.Overridden, schema.Combined} {
		s := schema.New()
		testtypes.Fill(s.Registry)
		i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
		i.Child("description {{ text:rest }}").
			Card(schema.ZeroToOne).MarkIdempotent()
		donor := parsePart(t, s, "interface eth1\n  description donor\n")
		borrowed := donor.Root.Children[0].Children[0]
		lend := func(_, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
			return borrowed, out
		}
		got, d := mergeText(t, s, merge.Options{Resolve: lend},
			"interface eth1\n  description uplink\n",
			"interface eth1\n  description downlink\n")
		assert.True(t, d.HasErrors())
		assert.Contains(t, d.String(), "already in a tree")
		assert.Contains(t, got, "description uplink")
	}
}

func TestMergeResolverCannotReplaceSectionWithFreshNode(t *testing.T) {
	rehead := func(earlier, _ *schema.Node, _ schema.MergeStrategy) (*schema.Node, schema.Outcome) {
		n := earlier.CloneValue()
		n.Fields = map[string]string{"asn": "65003"}
		n.Text = n.Def.Render(n.Fields)
		return n, schema.Overridden
	}
	got, d := mergeText(t, testSchema(), merge.Options{Resolve: rehead},
		"router bgp 65001\n  neighbor 10.0.0.1 remote-as 65001\n",
		"router bgp 65002\n  neighbor 10.0.0.2 remote-as 65002\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "childless fresh node")
	assert.Equal(t,
		"router bgp 65001\n  neighbor 10.0.0.1 remote-as 65001\n", got)
}

func TestMergeCombinedKeepingEarlierReportsLostValue(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	i.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent().
		MergeFunc(func(earlier, _ *schema.Node) (*schema.Node, schema.Outcome) {
			return earlier, schema.Combined
		})
	got, d := mergeText(t, s, declared,
		"interface eth1\n  description uplink\n",
		"interface eth1\n  description downlink\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "combined without part 2's value")
	assert.Contains(t, d.String(), `(was "description downlink")`)
	assert.Contains(t, got, "description uplink")
}
