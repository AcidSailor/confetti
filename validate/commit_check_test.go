package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/schema"
)

func refSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).
		Ref("vlan", "vlan.id")
	return s
}

func TestCommitCheckDanglingRef(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		refSchema(),
		"interface Ethernet1/1\n  switchport access vlan 10\n",
		parse.Reject,
		d,
	)
	CommitCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `vlan "10" does not exist`)
}

func TestCommitCheckResolvedRef(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		refSchema(),
		"vlan 10\ninterface Ethernet1/1\n  switchport access vlan 10\n",
		parse.Reject,
		d,
	)
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func multiRefSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("route-map {{ name:word }} {{ action:word }} {{ seq:uint }}").
		Card(schema.ZeroToN).Kind("route-map").Key("name", "action", "seq")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).Ref("vlan", "vlan.id")
	bgp := s.Node("router bgp {{ asn:asn }}").Card(schema.ZeroToOne)
	bgp.Child("neighbor {{ peer:ipv4 }} route-map {{ rmap:word }} {{ dir:word }}").
		Card(schema.ZeroToN).
		Ref("rmap", "route-map.name")
	return s
}

func TestCommitCheckCollectsMultiple(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport access vlan 10\n" +
		"router bgp 65000\n  neighbor 1.1.1.1 route-map MISSING in\n"
	cfg := parse.Parse(multiRefSchema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.Contains(t, d.String(), `vlan "10" does not exist`)
	assert.Contains(t, d.String(), `route-map "MISSING" does not exist`)
}

// A reference can target one component of a composite key.
func TestCommitCheckMultiKeyRefResolves(t *testing.T) {
	d := diag.New()
	in := "route-map FOO permit 10\nroute-map FOO permit 20\n" +
		"router bgp 65000\n  neighbor 1.1.1.1 route-map FOO in\n"
	cfg := parse.Parse(multiRefSchema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

// CommitCheck reports malformed lists directly because it can run without ImportCheck.
func TestCommitCheckMalformedListReportsInvalidList(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("allowed vlan {{ vlans:word }}").
		List("vlans", "vlan").
		Ref("vlans", "vlan.id")
	d := diag.New()
	cfg := parse.Parse(s, "allowed vlan 10,,20\n", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `invalid vlans "10,,20"`)
	assert.NotContains(t, d.String(), "does not exist")
}

func requiresSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("feature bgp").Card(schema.ZeroToOne).Kind("feature-bgp")
	s.Node("router bgp {{ asn:asn }}").
		Card(schema.ZeroToOne).Requires("feature-bgp")
	return s
}

func TestCommitCheckRequiresMissing(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		requiresSchema(), "router bgp 65000\n", parse.Reject, d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `requires a feature-bgp instance`)
	assert.Equal(t, 1, d.Items[0].Line, "points at the requiring line")
}

func TestCommitCheckRequiresSatisfied(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		requiresSchema(),
		"feature bgp\nrouter bgp 65000\n",
		parse.Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRequiresAbsentBothIsFine(t *testing.T) {
	// Requires is existential, not mandatory.
	d := diag.New()
	cfg := parse.Parse(requiresSchema(), "", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

// l2l3Schema prevents switched and routed lines from sharing an interface.
func l2l3Schema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:word }}").
		Card(schema.ZeroToN).Key("name")
	iface.Child("switchport").Card(schema.ZeroToOne).
		Tag("l2").ExcludeTag("l3")
	iface.Child("switchport access vlan {{ vlan:uint }}").
		Card(schema.ZeroToOne).Tag("l2").ExcludeTag("l3")
	iface.Child("ip address {{ addr:word }}").
		Card(schema.ZeroToOne).Tag("l3").ExcludeTag("l2")
	hsrp := iface.Child("hsrp {{ grp:uint }}").Card(schema.ZeroToN).Key("grp")
	hsrp.Child("ip {{ vip:word }}").Card(schema.ZeroToOne).Tag("l2")
	return s
}

func TestCommitCheckExcludeTagConflict(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport\n" +
		"  switchport access vlan 20\n  ip address 10.0.0.1/24\n"
	cfg := parse.Parse(l2l3Schema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(
		t, d.String(), `mutually exclusive with "switchport" (line 2)`,
	)
	assert.Contains(t, d.String(), `via label "l2"`)
	assert.Contains(t, d.String(), `via label "l3"`)
}

func TestCommitCheckExcludeTagSameSetCoexists(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport\n  switchport access vlan 20\n"
	cfg := parse.Parse(l2l3Schema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckExcludeTagIgnoresGrandchildren(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  ip address 10.0.0.1/24\n" +
		"  hsrp 1\n    ip 10.0.0.254\n"
	cfg := parse.Parse(l2l3Schema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckExcludeTagScopedToParentInstance(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport\n" +
		"interface Ethernet1/2\n  ip address 10.0.0.1/24\n"
	cfg := parse.Parse(l2l3Schema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRequiresSatisfiedByTag(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("feature lacp").Card(schema.ZeroToOne).Tag("feature-lacp")
	s.Node("port-channel {{ id:uint }}").
		Card(schema.ZeroToN).Key("id").Requires("feature-lacp")
	d := diag.New()
	cfg := parse.Parse(
		s, "feature lacp\nport-channel 10\n", parse.Reject, d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRefResolvesThroughTag(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").
		Card(schema.ZeroToN).Kind("vlan").Tag("bridge").Key("id")
	s.Node("member {{ vlan:vlan }}").
		Card(schema.ZeroToN).Key("vlan").Ref("vlan", "bridge.id")
	d := diag.New()
	cfg := parse.Parse(
		s, "vlan 10\nmember 10\nmember 20\n", parse.Reject, d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `bridge "20" does not exist`)
	assert.NotContains(t, d.String(), `"10" does not exist`)
}

func TestCommitCheckDanglingRefCarriesReferrerLine(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		refSchema(),
		"interface Ethernet1/1\n  switchport access vlan 10\n",
		parse.Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Equal(t, 2, d.Items[0].Line, "points at the referring line")
}

// checkText runs import-time and commit-time checks.
func checkText(t *testing.T, s *schema.Schema, in string) *diag.Diagnostics {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, in, parse.Reject, d)
	CommitCheck(cfg, d)
	return d
}

func TestCommitCheckExcludeTagAtTopLevel(t *testing.T) {
	s := schema.New()
	s.Node("feature fabricpath").Card(schema.ZeroToOne).
		Tag("fabric").ExcludeTag("overlay")
	s.Node("feature nv overlay").Card(schema.ZeroToOne).
		Tag("overlay").ExcludeTag("fabric")
	d := checkText(t, s, "feature fabricpath\nfeature nv overlay\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `via label "overlay"`)
}

func TestCommitCheckExcludeTagUnknownLabelIsError(t *testing.T) {
	s := schema.New()
	iface := s.Node("interface {{ name:word }}").
		Card(schema.ZeroToN).
		Key("name")
	iface.Child("ip address {{ a:word }}").Card(schema.ZeroToOne).
		ExcludeTag("l2")
	d := checkText(t, s, "interface Ethernet1\n  ip address 10.0.0.1/24\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `undeclared label "l2"`)
}

func TestCommitCheckRequiresUnknownLabelIsError(t *testing.T) {
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("nosuch")
	d := checkText(t, s, "router bgp\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `undeclared label "nosuch"`)
}

func TestCommitCheckTagCollidingWithKindIsError(t *testing.T) {
	s := schema.New()
	s.Node("vlan {{ id:uint }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("bogus {{ id:uint }}").Card(schema.ZeroToN).Key("id").Tag("vlan")
	d := checkText(t, s, "vlan 10\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `Tag "vlan" collides with a Kind`)
}

func TestCommitCheckRefUnknownTargetKeyIsError(t *testing.T) {
	s := schema.New()
	s.Node("grp {{ id:uint }}").Card(schema.ZeroToN).Key("id").Tag("bridge")
	s.Node("member {{ v:uint }}").Card(schema.ZeroToN).Key("v").
		Ref("v", "bridge.nosuch")
	d := checkText(t, s, "grp 10\nmember 10\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`no definition carrying "bridge" keys by "nosuch"`,
	)
}

func TestCommitCheckHalfSpecifiedRelationIsError(t *testing.T) {
	s := schema.New()
	n := s.Node("member {{ v:uint }}").Card(schema.ZeroToN).Key("v").Tag("m")
	n.Relations = append(n.Relations, schema.Relation{Label: "m", FromArg: "v"})
	d := checkText(t, s, "member 10\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "matches arg \"v\" against no target key")
}

func TestCommitCheckInvalidRelationEnumsAreErrors(t *testing.T) {
	s := schema.New()
	n := s.Node("feature").Tag("feature")
	n.Relations = append(n.Relations,
		schema.Relation{Label: "feature", Scope: schema.Scope(2)},
		schema.Relation{Label: "feature", Want: schema.Polarity(2)},
	)
	d := checkText(t, s, "feature\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `invalid scope 2`)
	assert.Contains(t, d.String(), `invalid polarity 2`)
}

func TestCommitCheckUnsupportedRelationShapesAreErrors(t *testing.T) {
	s := schema.New()
	n := s.Node("feature").Tag("feature")
	n.Relations = append(n.Relations,
		schema.Relation{
			Label: "feature",
			Scope: schema.ScopeTree,
			Want:  schema.Absent,
		},
		schema.Relation{
			Label: "feature",
			Scope: schema.ScopeSiblings,
			Want:  schema.Present,
		},
	)
	d := checkText(t, s, "feature\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Equal(t, 2, strings.Count(d.String(), "is unsupported"), d.String())
}
