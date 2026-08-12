package remediate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

// kindSlotSchema models issue #1: variant spellings of one slot declared with a shared Kind.
func kindSlotSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.ZeroToOne).Kind("default-originate")
	r.Child("default-originate").
		Card(schema.ZeroToOne).Kind("default-originate")
	r.Child("router-id {{ ip:ipv4 }}").Card(schema.ZeroToOne).Kind("router-id")
	r.Child("local-as {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("send-community").Card(schema.ZeroToOne)
	r.Child("send-community extended").Card(schema.ZeroToOne)
	return s
}

// idempotentVariantSchema marks only the route-map spelling idempotent so the two directions take different paths.
func idempotentVariantSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.ZeroToOne).Kind("default-originate").MarkIdempotent()
	r.Child("default-originate").
		Card(schema.ZeroToOne).Kind("default-originate")
	return s
}

// toggleKindSchema declares a toggle pair whose members also share a Kind.
func toggleKindSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	shut := iface.Child("shutdown").Card(schema.ZeroToOne).Kind("shutdown")
	iface.Child("no shutdown").Card(schema.ZeroToOne).Kind("shutdown").
		Toggles(shut)
	return s
}

// rawDiff runs Diff without failing on diagnostics so refusal paths can be asserted.
func rawDiff(
	t *testing.T,
	s *schema.Schema,
	running, intended string,
) (string, *diag.Diagnostics) {
	t.Helper()
	res, d := Diff(
		mustParse(t, s, running),
		mustParse(t, s, intended),
		diag.Policy{},
	)
	return render.Render(res.Tree), d
}

// kindSlotDiff runs rawDiff and fails the test on any diagnostic error.
func kindSlotDiff(
	t *testing.T,
	s *schema.Schema,
	running, intended string,
) (string, *diag.Diagnostics) {
	t.Helper()
	out, d := rawDiff(t, s, running, intended)
	require.False(t, d.HasErrors(), d.String())
	return out, d
}

func TestKindSlotSpellingChangeNegatesThenAdds(t *testing.T) {
	out, _ := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.Equal(
		t,
		"router bgp 65000\n  no default-originate\n  default-originate route-map RM\n",
		out,
	)
}

func TestKindSlotSpellingChangeReverse(t *testing.T) {
	out, _ := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  default-originate route-map RM\n",
		"router bgp 65000\n  default-originate\n")
	assert.Equal(
		t,
		"router bgp 65000\n  no default-originate route-map RM\n  default-originate\n",
		out,
	)
}

func TestKindSlotIdempotentVariantReissues(t *testing.T) {
	out, _ := kindSlotDiff(t, idempotentVariantSchema(),
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.Equal(t,
		"router bgp 65000\n  default-originate route-map RM\n",
		out)
}

func TestKindSlotValueChangeIsReplace(t *testing.T) {
	out, _ := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  router-id 10.0.0.1\n",
		"router bgp 65000\n  router-id 10.0.0.2\n")
	assert.Equal(t,
		"router bgp 65000\n  no router-id 10.0.0.1\n  router-id 10.0.0.2\n",
		out)
}

func TestKindSlotUnrelatedSiblingsStayIndependent(t *testing.T) {
	out, d := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  send-community\n",
		"router bgp 65000\n  send-community\n  send-community extended\n")
	assert.Equal(t, "router bgp 65000\n  send-community extended\n", out)
	assert.Empty(t, d.String())
}

func TestKindSlotUnchangedSpellingIsNoOp(t *testing.T) {
	out, _ := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  default-originate route-map RM\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.Equal(t, "", out)
}

func TestSplitSlotWarningFiresWithoutKind(t *testing.T) {
	// local-as has no Kind, so a value change still splits into Add and Remove; the destructive order is pinned so a future pairing fix shows as a diff.
	out, d := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  local-as 65001\n",
		"router bgp 65000\n  local-as 65002\n")
	assert.Equal(t,
		"router bgp 65000\n  local-as 65002\n  no local-as 65001\n", out)
	assert.Contains(t, d.String(),
		"single-occupancy slot \"local-as 65001\";"+
			" give the definition a Kind or MarkIdempotent")
}

func TestSplitSlotWarningSilentForUnrelatedAddRemove(t *testing.T) {
	_, d := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  send-community\n",
		"router bgp 65000\n  send-community extended\n")
	assert.Empty(t, d.String())
}

func TestKindSlotToggleMembersKeepFlip(t *testing.T) {
	// A toggle member with a Kind keeps flip semantics: the add supersedes the remove.
	out, _ := kindSlotDiff(t, toggleKindSchema(),
		"interface Ethernet1/1\n  shutdown\n",
		"interface Ethernet1/1\n  no shutdown\n")
	assert.Equal(t, "interface Ethernet1/1\n  no shutdown\n", out)
}

func TestCategoryOfKindedSingle(t *testing.T) {
	s := kindSlotSchema()
	cfg := mustParse(t, s,
		"router bgp 65000\n  default-originate\n  local-as 65001\n")
	bgp := topNode(cfg, 0)
	do, las := bgp.Children[0], bgp.Children[1]

	assert.Equal(t, ident.KindedSingle, ident.CategoryOf(do))
	assert.Equal(t, ident.FullLine, ident.CategoryOf(las))

	// Both spellings share one identity; the captured value is excluded.
	other := mustParse(t, s,
		"router bgp 65000\n  default-originate route-map RM\n").
		Root.Children[0].Children[0]
	assert.Equal(t, ident.Of(do), ident.Of(other))
}

func TestCategoryOfToggleMemberWithKindStaysFullLine(t *testing.T) {
	cfg := mustParse(
		t,
		toggleKindSchema(),
		"interface Ethernet1/1\n  shutdown\n",
	)
	assert.Equal(t, ident.FullLine,
		ident.CategoryOf(cfg.Root.Children[0].Children[0]))
}

// kindSectionSchema pairs a leaf spelling and a section spelling of one slot.
func kindSectionSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	sec := r.Child("default-originate route-map {{ m:word }}").
		Card(schema.ZeroToOne).Kind("do")
	sec.Child("weight {{ w:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate").Card(schema.ZeroToOne).Kind("do")
	return s
}

func TestKindSlotSectionCrossDefReplace(t *testing.T) {
	out, _ := kindSlotDiff(t, kindSectionSchema(),
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n    weight 100\n")
	assert.Equal(t,
		"router bgp 65000\n  no default-originate\n"+
			"  default-originate route-map RM\n    weight 100\n",
		out)

	// The reverse direction negates the section header once, never its children.
	out, _ = kindSlotDiff(t, kindSectionSchema(),
		"router bgp 65000\n  default-originate route-map RM\n    weight 100\n",
		"router bgp 65000\n  default-originate\n")
	assert.Equal(t,
		"router bgp 65000\n  no default-originate route-map RM\n"+
			"  default-originate\n",
		out)
}

func TestKindSlotSectionValueChangeReplaces(t *testing.T) {
	// A Kind-paired section header has no key, so a changed header value must negate the old section, not reissue the header.
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	tpl := r.Child("template peer {{ n:word }}").
		Card(schema.ZeroToOne).Kind("tpl")
	tpl.Child("remote-as {{ a:asn }}").Card(schema.ZeroToOne)
	out, _ := kindSlotDiff(t, s,
		"router bgp 65000\n  template peer A\n    remote-as 1\n",
		"router bgp 65000\n  template peer B\n    remote-as 1\n")
	assert.Equal(t,
		"router bgp 65000\n  no template peer A\n"+
			"  template peer B\n    remote-as 1\n",
		out)
}

func TestKindSlotReplaceProtectedDescendantRefused(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	sec := r.Child("default-originate route-map {{ m:word }}").
		Card(schema.ZeroToOne).Kind("do")
	sec.Child("weight {{ w:asn }}").Card(schema.ZeroToOne).Protect()
	r.Child("default-originate").Card(schema.ZeroToOne).Kind("do")
	out, d := rawDiff(t, s,
		"router bgp 65000\n  default-originate route-map RM\n    weight 100\n",
		"router bgp 65000\n  default-originate\n")
	assert.True(t, d.HasErrors(), "out: %s", out)
	assert.Contains(t, d.String(), "refusing to replace protected")
}

func TestKindSlotReplaceFreesUniqueBeforeClaim(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vrf {{ name:word }}").Card(schema.ZeroToN).Kind("vrf").
		Key("name").Unique("name")
	a := s.Node("mgmt-a").Card(schema.ZeroToOne).Kind("mgmt")
	b := s.Node("mgmt-b").Card(schema.ZeroToOne).Kind("mgmt")
	a.Child("vrf {{ name:word }}").Card(schema.ZeroToN).Kind("vrf").
		Key("name").Unique("name")
	b.Child("vrf {{ name:word }}").Card(schema.ZeroToN).Kind("vrf").
		Key("name").Unique("name")
	out, _ := kindSlotDiff(t, s,
		"mgmt-a\n  vrf RED\n",
		"vrf RED\nmgmt-b\n")
	// The replace that frees vrf RED must render before the root claim.
	assert.Equal(t, "no mgmt-a\nmgmt-b\nvrf RED\n", out)
}

func TestSplitSlotWarningSilentForKeptToggleRemoval(t *testing.T) {
	// A toggle removal kept because of a protected child must not draw the split warning; the Protect error already fires.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	en := iface.Child("logging enable").Kind("log")
	en.Child("secret {{ w:word }}").Card(schema.ZeroToOne).Protect()
	iface.Child("logging disable").Kind("log").Toggles(en)
	_, d := rawDiff(t, s,
		"interface Ethernet1/1\n  logging enable\n    secret S\n",
		"interface Ethernet1/1\n  logging disable\n")
	assert.True(t, d.HasErrors())
	assert.NotContains(t, d.String(), "single-occupancy")
}

func TestKindSlotIdempotentVariantReverseReplaces(t *testing.T) {
	// Only the intended definition's idempotency selects the reissue path; the reverse direction is a full Replace.
	out, _ := kindSlotDiff(t, idempotentVariantSchema(),
		"router bgp 65000\n  default-originate route-map RM\n",
		"router bgp 65000\n  default-originate\n")
	assert.Equal(t,
		"router bgp 65000\n  no default-originate route-map RM\n"+
			"  default-originate\n",
		out)
}

func TestKindSlotProtectedRunningRefusesCrossDefReissue(t *testing.T) {
	// A Kind-paired reissue destroys the running line, so Protect must refuse it exactly as it refuses a Replace.
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ m:word }}").
		Card(schema.ZeroToOne).Kind("do").MarkIdempotent()
	r.Child("default-originate").Card(schema.ZeroToOne).Kind("do").Protect()
	out, d := rawDiff(t, s,
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "refusing to replace protected")
	assert.Empty(t, out)
}

func TestKindSlotRunningCarriesBothSpellings(t *testing.T) {
	// Cleanup must reissue the intended spelling after negating the stale duplicate.
	tests := []struct {
		name, intended, want string
	}{
		{
			name:     "first spelling intended",
			intended: "router bgp 65000\n  default-originate\n",
			want: "router bgp 65000\n  no default-originate route-map RM\n" +
				"  default-originate\n",
		},
		{
			name:     "second spelling intended",
			intended: "router bgp 65000\n  default-originate route-map RM\n",
			want: "router bgp 65000\n  no default-originate\n" +
				"  default-originate route-map RM\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, d := rawDiff(t, kindSlotSchema(),
				"router bgp 65000\n  default-originate\n"+
					"  default-originate route-map RM\n",
				tt.intended)
			assert.Equal(t, tt.want, out)
			assert.Empty(t, d.String())
		})
	}
}

func TestKindSlotRunningCarriesThreeSpellings(t *testing.T) {
	// Every extra stale spelling must be negated before the intended value is reissued.
	out, d := rawDiff(t, kindSlotSchema(),
		"router bgp 65000\n  default-originate route-map OLD\n"+
			"  default-originate\n  default-originate route-map RM\n",
		"router bgp 65000\n  default-originate route-map RM\n")
	assert.Equal(t,
		"router bgp 65000\n  no default-originate\n"+
			"  no default-originate route-map OLD\n"+
			"  default-originate route-map RM\n",
		out)
	assert.Empty(t, d.String())
}

func TestKindSlotIntendedCarriesBothSpellings(t *testing.T) {
	// Two intended spellings make the goal ambiguous, so only the first applies and the policy sets the severity.
	s := kindSlotSchema()
	res, d := Diff(
		mustParse(t, s,
			"router bgp 65000\n  default-originate route-map RM\n"),
		mustParse(t, s, "router bgp 65000\n"+
			"  default-originate route-map RM2\n  default-originate\n"),
		diag.Policy{Strict: true},
	)
	assert.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "duplicate spelling")
	// The stale line is negated once, not once per intended spelling.
	assert.Equal(t,
		"router bgp 65000\n  no default-originate route-map RM\n"+
			"  default-originate route-map RM2\n",
		render.Render(res.Tree))
}

func TestKindOnClearOnRemoveSectionKeepsExpandRemove(t *testing.T) {
	// A Kind added for Ref resolution must not enrol an EmptyOnRemove section in slot pairing; it has no header negation.
	build := func(kind bool) *schema.Schema {
		s := schema.New()
		testtypes.Fill(s.Registry)
		v := s.Node("vlan database {{ n:word }}").
			Card(schema.ZeroToOne).ClearOnRemove()
		v.Child("vlan {{ id:vlan }}").Key("id")
		if kind {
			v.Kind("vdb")
		}
		return s
	}
	for _, kind := range []bool{false, true} {
		out, d := rawDiff(t, build(kind),
			"vlan database A\n  vlan 10\n", "vlan database B\n  vlan 10\n")
		assert.Equal(t,
			"vlan database A\n  no vlan 10\nvlan database B\n  vlan 10\n",
			out, "kind=%v", kind)
		assert.Empty(t, d.String(), "kind=%v", kind)
	}
}

func TestSplitSlotStrictIsError(t *testing.T) {
	// The split empties the slot on the device, so a strict policy must reject the plan instead of warning.
	s := kindSlotSchema()
	_, d := Diff(
		mustParse(t, s, "router bgp 65000\n  local-as 65001\n"),
		mustParse(t, s, "router bgp 65000\n  local-as 65002\n"),
		diag.Policy{Strict: true},
	)
	assert.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "split single-occupancy slot")
}

func TestSplitSlotToggleSharingKindWithSiblingReported(t *testing.T) {
	// A toggle member and a non-toggle sibling sharing a Kind are not a declared flip, so the split must be reported.
	s := schema.New()
	testtypes.Fill(s.Registry)
	i := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	en := i.Child("logging enable").Card(schema.ZeroToOne).Kind("log")
	i.Child("logging disable").Card(schema.ZeroToOne).Kind("log").Toggles(en)
	i.Child("logging level {{ l:asn }}").Card(schema.ZeroToOne).Kind("log")
	out, d := rawDiff(t, s,
		"interface Ethernet1/1\n  logging enable\n",
		"interface Ethernet1/1\n  logging level 3\n")
	assert.Equal(t,
		"interface Ethernet1/1\n  logging level 3\n  no logging enable\n", out)
	assert.Contains(t, d.String(), "split single-occupancy slot")
}

func TestKindSlotReplaceEmitsRunningRefDiagnosticOnce(t *testing.T) {
	// A Replace's removed subtree is its runSrc, so the ref walk must not visit it twice and double the diagnostic.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	for _, tmpl := range []string{"profile A", "profile B"} {
		s.Node(tmpl).Card(schema.ZeroToOne).Kind("prof").
			Child("trunk allowed {{ l:word }}").Card(schema.ZeroToOne).
			List("l", "vlan").Ref("l", "vlan.id")
	}
	_, d := rawDiff(t, s, "profile A\n  trunk allowed 10-\n", "profile B\n")
	assert.Equal(t, 1,
		strings.Count(d.String(), "unresolvable list"), d.String())
}

func TestKindOnManyStaysIndependent(t *testing.T) {
	// Multi-occupancy siblings sharing a Kind must not collapse to one slot, which would silently negate all but the first.
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("network {{ n:word }}").Card(schema.ZeroToN).Kind("net")
	out, d := rawDiff(t, s,
		"router bgp 65000\n  network A\n  network B\n",
		"router bgp 65000\n  network A\n  network C\n")
	assert.Equal(t,
		"router bgp 65000\n  network C\n  no network B\n", out)
	assert.Empty(t, d.String())
}

func TestKeyedSingleWithKindStaysKeyed(t *testing.T) {
	// A keyed single-occupancy node keeps key identity; pairing it by Kind alone would merge unrelated keys.
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("peer {{ n:word }}").Card(schema.ZeroToOne).Kind("pk").Key("n")
	cfg := mustParse(t, s, "router bgp 65000\n  peer A\n")
	peer := cfg.Root.Children[0].Children[0]
	assert.Equal(t, ident.Keyed, ident.CategoryOf(peer))
	// Different keys are unrelated nodes, so the change is an add plus a remove, never a Replace.
	out, _ := rawDiff(t, s,
		"router bgp 65000\n  peer A\n", "router bgp 65000\n  peer B\n")
	assert.Equal(t, "router bgp 65000\n  peer B\n  no peer A\n", out)
}
