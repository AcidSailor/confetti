package remediate

import (
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

func kindSlotDiff(
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
	require.False(t, d.HasErrors(), d.String())
	return render.Render(res.Tree), d
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
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.ZeroToOne).Kind("default-originate").MarkIdempotent()
	r.Child("default-originate").
		Card(schema.ZeroToOne).Kind("default-originate")
	out, _ := kindSlotDiff(t, s,
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
	// local-as has no Kind, so a value change still splits into Add and Remove and must warn.
	out, d := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  local-as 65001\n",
		"router bgp 65000\n  local-as 65002\n")
	assert.Contains(t, out, "local-as 65002")
	assert.Contains(t, out, "no local-as 65001")
	assert.Contains(t, d.String(), "single-occupancy")
}

func TestSplitSlotWarningSilentForUnrelatedAddRemove(t *testing.T) {
	_, d := kindSlotDiff(t, kindSlotSchema(),
		"router bgp 65000\n  send-community\n",
		"router bgp 65000\n  send-community extended\n")
	assert.Empty(t, d.String())
}

func TestKindSlotToggleMembersKeepFlip(t *testing.T) {
	// A toggle member with a Kind keeps flip semantics: the add supersedes the remove.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	shut := iface.Child("shutdown").Card(schema.ZeroToOne).Kind("shutdown")
	iface.Child("no shutdown").Card(schema.ZeroToOne).Kind("shutdown").
		Toggles(shut)
	out, _ := kindSlotDiff(t, s,
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
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	shut := iface.Child("shutdown").Card(schema.ZeroToOne).Kind("shutdown")
	iface.Child("no shutdown").Card(schema.ZeroToOne).Kind("shutdown").
		Toggles(shut)
	cfg := mustParse(t, s, "interface Ethernet1/1\n  shutdown\n")
	assert.Equal(t, ident.FullLine,
		ident.CategoryOf(cfg.Root.Children[0].Children[0]))
}
