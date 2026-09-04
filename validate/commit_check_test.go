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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRequiresAbsentBothIsFine(t *testing.T) {
	// Requires is existential, not mandatory.
	d := diag.New()
	cfg := parse.Parse(requiresSchema(), "", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckExcludeTagIgnoresGrandchildren(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  ip address 10.0.0.1/24\n" +
		"  hsrp 1\n    ip 10.0.0.254\n"
	cfg := parse.Parse(l2l3Schema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, nil, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckExcludeTagScopedToParentInstance(t *testing.T) {
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport\n" +
		"interface Ethernet1/2\n  ip address 10.0.0.1/24\n"
	cfg := parse.Parse(l2l3Schema(), in, parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
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
	CommitCheck(cfg, nil, d)
	require.True(t, d.HasErrors())
	assert.Equal(t, 2, d.Items[0].Line, "points at the referring line")
}

// checkText runs import-time and commit-time checks.
func checkText(t *testing.T, s *schema.Schema, in string) *diag.Diagnostics {
	t.Helper()
	d := diag.New()
	cfg := parse.Parse(s, in, parse.Reject, d)
	CommitCheck(cfg, nil, d)
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

func TestCommitCheckBaselineResolvesRef(t *testing.T) {
	d := diag.New()
	s := refSchema()
	baseline := parse.Parse(s, "vlan 1\n", parse.Reject, d)
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  switchport access vlan 1\n",
		parse.Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, baseline, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckBaselineSatisfiesRequires(t *testing.T) {
	d := diag.New()
	s := requiresSchema()
	baseline := parse.Parse(s, "feature bgp\n", parse.Reject, d)
	cfg := parse.Parse(s, "router bgp 65000\n", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, baseline, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckBaselineRelationsAreNotChecked(t *testing.T) {
	// A baseline line with a dangling reference is a target, not a subject.
	d := diag.New()
	s := refSchema()
	baseline := parse.Parse(
		s,
		"interface Ethernet1/9\n  switchport access vlan 99\n",
		parse.Reject,
		d,
	)
	cfg := parse.Parse(s, "vlan 10\n", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, baseline, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckBaselineSchemaMismatchIsError(t *testing.T) {
	d := diag.New()
	baseline := parse.Parse(refSchema(), "vlan 1\n", parse.Reject, d)
	cfg := parse.Parse(
		refSchema(),
		"interface Ethernet1/1\n  switchport access vlan 1\n",
		parse.Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, baseline, d)
	require.True(t, d.HasErrors())
	assert.Contains(
		t,
		d.String(),
		"baseline and configuration use different schemas",
	)
	// The unusable baseline is dropped, but the tree is still checked.
	assert.Contains(t, d.String(), `vlan "1" does not exist`)
}

func TestCommitCheckNamespaceAcrossKindsIsValid(t *testing.T) {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	// Namespace before Tag must validate the same as after it.
	s.Node("mac access-list {{ name:word }}").Card(schema.ZeroToN).
		Namespace("acl").Kind("mac-acl").Tag("acl").Key("name")
	d := checkText(t, s, "ip access-list A\nmac access-list B\n")
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckNamespaceNotCarriedIsError(t *testing.T) {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Key("name").Namespace("acl")
	s.Node("mac access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("mac-acl").Tag("acl").Key("name").Namespace("acl")
	d := checkText(t, s, "ip access-list A\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`Namespace "acl" is not a Kind or Tag of this definition`,
	)
}

func TestCommitCheckNamespaceOnKeylessDefIsError(t *testing.T) {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	s.Node("mac access-list").Card(schema.ZeroToOne).
		Kind("mac-acl").Tag("acl").Namespace("acl")
	d := checkText(t, s, "ip access-list A\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `Namespace "acl" needs a Key`)
}

func TestCommitCheckNamespaceWithOneMemberIsError(t *testing.T) {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	d := checkText(t, s, "ip access-list A\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `Namespace "acl" has no other keyed member`)
}

func TestCommitCheckNamespaceHalfDeclaredIsError(t *testing.T) {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	s.Node("mac access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("mac-acl").Tag("acl").Key("name")
	d := checkText(t, s, "ip access-list A\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`mac access-list {{ name:word }}: carries label "acl" used as a Namespace but does not declare Namespace("acl")`,
	)
}

func TestCommitCheckNamespaceArityMismatchIsError(t *testing.T) {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	s.Node("mac access-list {{ name:word }} {{ seq:uint }}").
		Card(schema.ZeroToN).
		Kind("mac-acl").
		Tag("acl").
		Key("name", "seq").
		Namespace("acl")
	d := checkText(t, s, "ip access-list A\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`Namespace "acl" members disagree on exclusive arg count`,
	)
}

// aclNamespaceSchema returns two Kinds that share one device-side name space.
func aclNamespaceSchema() *schema.Schema {
	s := schema.New()
	s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	s.Node("mac access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("mac-acl").Tag("acl").Key("name").Namespace("acl")
	return s
}

func TestCommitCheckNamespaceCollisionIsError(t *testing.T) {
	d := checkText(t, aclNamespaceSchema(),
		"ip access-list L\nmac access-list L\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`mac access-list L: name "L" under label "acl" is already held by "ip access-list L" (line 1)`,
	)
	assert.Equal(t, 2, d.Items[0].Line, "points at the later holder")
}

func TestCommitCheckNamespaceDistinctNamesCoexist(t *testing.T) {
	d := checkText(t, aclNamespaceSchema(),
		"ip access-list L\nmac access-list M\n")
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckNamespaceSameObjectReenteredIsNotCollision(t *testing.T) {
	d := checkText(t, aclNamespaceSchema(),
		"ip access-list L\nip access-list L\n")
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckNamespaceCollisionWithBaselineIsError(t *testing.T) {
	d := diag.New()
	s := aclNamespaceSchema()
	baseline := parse.Parse(s, "ip access-list L\n", parse.Reject, d)
	cfg := parse.Parse(s, "mac access-list L\n", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, baseline, d)
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`name "L" under label "acl" is already held by baseline "ip access-list L"`,
	)
}

// prioNamespaceSchema returns two Kinds whose exclusive name is a list of ids.
func prioNamespaceSchema() *schema.Schema {
	s := schema.New()
	s.Node("ip prio {{ ids:word }} value {{ v:word }}").Card(schema.ZeroToN).
		Kind("ip-prio").Tag("prio").Key("v").Unique("ids").
		List("ids", "uint").Namespace("prio")
	s.Node("mac prio {{ ids:word }} value {{ v:word }}").Card(schema.ZeroToN).
		Kind("mac-prio").Tag("prio").Key("v").Unique("ids").
		List("ids", "uint").Namespace("prio")
	return s
}

func TestCommitCheckListMembersOverlapIsError(t *testing.T) {
	d := checkText(t, prioNamespaceSchema(),
		"ip prio 1-3 value a\nmac prio 2 value b\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `under label "prio" is already held by`)
}

func TestCommitCheckListMembersDisjointCoexist(t *testing.T) {
	d := checkText(t, prioNamespaceSchema(),
		"ip prio 1-3 value a\nmac prio 4-5 value b\n")
	assert.False(t, d.HasErrors(), d.String())
}

// Set-equal spellings must reach the same verdict; comparing raw text would not.
func TestCommitCheckListSpellingDoesNotChangeVerdict(t *testing.T) {
	same := checkText(t, prioNamespaceSchema(),
		"ip prio 1-3 value a\nmac prio 3,2,1 value b\n")
	require.True(t, same.HasErrors(), same.String())
	disjoint := checkText(t, prioNamespaceSchema(),
		"ip prio 1,2,3 value a\nmac prio 4 value b\n")
	assert.False(t, disjoint.HasErrors(), disjoint.String())
}

// A third claim must be checked against every earlier one, not only the first.
func TestCommitCheckListOverlapWithLaterHolderIsError(t *testing.T) {
	d := checkText(t, prioNamespaceSchema(),
		"ip prio 1 value a\nmac prio 5 value b\nip prio 5 value c\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`is already held by "mac prio 5 value b" (line 2)`,
	)
}

func TestCommitCheckUniqueCollisionUnderKindIsError(t *testing.T) {
	s := schema.New()
	s.Node("slot {{ id:word }} owner {{ own:word }}").Card(schema.ZeroToN).
		Kind("slot").Key("id", "own").Unique("id")
	d := checkText(t, s, "slot 1 owner a\nslot 1 owner b\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`slot 1 owner b: name "1" under label "slot" is already held by "slot 1 owner a" (line 1)`,
	)
}

func TestCommitCheckKeyedKindCollisionAcrossDefsIsError(t *testing.T) {
	s := schema.New()
	s.Node("vrf {{ name:word }}").Card(schema.ZeroToN).Kind("vrf").Key("name")
	s.Node("vrf context {{ name:word }}").Card(schema.ZeroToN).Kind("vrf").
		Key("name")
	d := checkText(t, s, "vrf RED\nvrf context RED\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `name "RED" under label "vrf"`)
}

func TestCommitCheckKeylessKindHoldsNoName(t *testing.T) {
	s := schema.New()
	r := s.Node("router bgp {{ as:word }}").Card(schema.ZeroToOne)
	r.Child("default-originate").Card(schema.ZeroToOne).Kind("do")
	r.Child("default-originate always").Card(schema.ZeroToOne).Kind("do")
	d := checkText(t, s,
		"router bgp 65000\n  default-originate\n  default-originate always\n")
	assert.False(t, d.HasErrors(), d.String())
}

// A keyed definition with neither Kind nor Namespace is exclusive in ordering, so it must be here too.
func TestCommitCheckUniqueCollisionWithoutKindIsError(t *testing.T) {
	s := schema.New()
	s.Node("slot {{ id:word }} owner {{ own:word }}").Card(schema.ZeroToN).
		Key("id", "own").Unique("id")
	d := checkText(t, s, "slot 1 owner a\nslot 1 owner b\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(
		t,
		d.String(),
		`name "1" under definition "slot {{ id:word }} owner {{ own:word }}"`,
	)
}

// nbrSchema declares one keyed Kind reachable under two different parents.
func nbrSchema() *schema.Schema {
	s := schema.New()
	bgp := s.Node("router bgp {{ as:word }}").Card(schema.ZeroToOne)
	bgp.Child("neighbor {{ ip:word }}").Card(schema.ZeroToN).
		Kind("nbr").Key("ip")
	vrf := bgp.Child("vrf {{ name:word }}").Card(schema.ZeroToN).
		Kind("bgp-vrf").Key("name")
	vrf.Child("neighbor {{ ip:word }}").Card(schema.ZeroToN).
		Kind("nbr").Key("ip")
	return s
}

// A Kind names one space per owner: a global and a VRF neighbour are distinct objects.
func TestCommitCheckKindNameIsScopedToItsOwner(t *testing.T) {
	d := checkText(t, nbrSchema(),
		"router bgp 1\n  neighbor 10.0.0.1\n  vrf red\n    neighbor 10.0.0.1\n")
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckKindCollisionUnderOneOwnerIsError(t *testing.T) {
	s := schema.New()
	r := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	r.Child("slot {{ id:word }} owner {{ own:word }}").Card(schema.ZeroToN).
		Kind("slot").Key("id", "own").Unique("id")
	d := checkText(t, s, "box a\n  slot 1 owner x\n  slot 1 owner y\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `name "1" under label "slot"`)
}

func TestCommitCheckKindCollisionAcrossOwnersIsNotAnError(t *testing.T) {
	s := schema.New()
	r := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	r.Child("slot {{ id:word }} owner {{ own:word }}").Card(schema.ZeroToN).
		Kind("slot").Key("id", "own").Unique("id")
	d := checkText(t, s, "box a\n  slot 1 owner x\nbox b\n  slot 1 owner y\n")
	assert.False(t, d.HasErrors(), d.String())
}

// A Namespace names one device-wide space, so it collides across owners.
func TestCommitCheckNamespaceNameSpansOwners(t *testing.T) {
	s := schema.New()
	r := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	r.Child("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name").Namespace("acl")
	r.Child("mac access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("mac-acl").Tag("acl").Key("name").Namespace("acl")
	d := checkText(
		t,
		s,
		"box a\n  ip access-list L\nbox b\n  mac access-list L\n",
	)
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `name "L" under label "acl"`)
}

// afNbrSchema puts the holder two levels below the object that opens its name space.
func afNbrSchema(scoped bool) *schema.Schema {
	s := schema.New()
	bgp := s.Node("router bgp {{ as:word }}").Card(schema.ZeroToOne)
	vrf := bgp.Child("vrf {{ name:word }}").Card(schema.ZeroToN).
		Kind("bgp-vrf").Key("name")
	af := vrf.Child("address-family {{ af:word }}").Card(schema.ZeroToN).
		Kind("af").Key("af")
	nbr := af.Child("neighbor {{ ip:word }}").Card(schema.ZeroToN).
		Kind("nbr").Key("ip")
	if scoped {
		nbr.ScopedBy(vrf)
	}
	return s
}

const twoAfOneVrf = "router bgp 1\n  vrf red\n" +
	"    address-family ipv4\n      neighbor 10.0.0.1\n" +
	"    address-family ipv6\n      neighbor 10.0.0.1\n"

// The parent is the address-family, but the name space belongs to the VRF.
func TestCommitCheckScopedByFindsCollisionAboveTheParent(t *testing.T) {
	d := checkText(t, afNbrSchema(true), twoAfOneVrf)
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `name "10.0.0.1" under label "nbr"`)
}

// Without the declaration the per-owner default cannot see past the address-family.
func TestCommitCheckUnscopedMissesCollisionAboveTheParent(t *testing.T) {
	d := checkText(t, afNbrSchema(false), twoAfOneVrf)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckScopedBySeparatesAnchorInstances(t *testing.T) {
	d := checkText(
		t,
		afNbrSchema(true),
		"router bgp 1\n  vrf red\n    address-family ipv4\n      neighbor 10.0.0.1\n"+
			"  vrf blue\n    address-family ipv4\n      neighbor 10.0.0.1\n",
	)
	assert.False(t, d.HasErrors(), d.String())
}

// ScopedByDevice makes one Kind exclusive device-wide without a second Namespace member.
func TestCommitCheckScopedByDeviceSpansOwners(t *testing.T) {
	s := schema.New()
	r := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	r.Child("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id").ScopedByDevice()
	d := checkText(t, s, "box a\n  claim 1\nbox b\n  claim 1\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `name "1" under label "claim"`)
}

func TestValidateRelationsScopeAnchorMustBeOnEveryPath(t *testing.T) {
	s := schema.New()
	dest := s.Node("destination").Card(schema.ZeroToOne)
	claim := dest.Child("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id")
	features := s.Node("features").Card(schema.ZeroToOne)
	features.Child("feature on").Card(schema.ZeroToOne).Adopt(claim)
	claim.ScopedBy(dest)
	d := checkText(t, s, "destination\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(),
		`ScopedBy anchor "destination" is not an ancestor on every path`)
}

func TestValidateRelationsScopeAnchorOnEveryPathIsValid(t *testing.T) {
	s := schema.New()
	dest := s.Node("destination").Card(schema.ZeroToOne)
	dest.Child("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id").ScopedBy(dest)
	d := checkText(t, s, "destination\n  claim 1\n")
	assert.False(t, d.HasErrors(), d.String())
}

func TestValidateRelationsScopeNeedsKeyAndOneExtent(t *testing.T) {
	s := schema.New()
	box := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	box.Child("flag").Card(schema.ZeroToOne).Kind("flag").ScopedByDevice()
	box.Child("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id").ScopedBy(box).ScopedByDevice()
	d := checkText(t, s, "box a\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(),
		"a declared exclusive scope needs a Key to hold a name")
	assert.Contains(t, d.String(),
		"ScopedBy and ScopedByDevice name different extents")
}

// An empty config and unmatched lines must degrade, not panic.
func TestCommitCheckToleratesEmptyConfigAndUnmatchedNodes(t *testing.T) {
	d := diag.New()
	assert.NotPanics(t, func() {
		CommitCheck(&schema.Config{Schema: aclNamespaceSchema()}, nil, d)
	})
	assert.NotPanics(t, func() { CommitCheck(&schema.Config{}, nil, d) })

	// A hand-built tree can carry a node that never matched a definition.
	cfg := schema.NewConfig(aclNamespaceSchema())
	cfg.Root.AddChild(schema.NewNode("who knows"))
	assert.NotPanics(t, func() { CommitCheck(cfg, nil, d) })
	assert.False(t, d.HasErrors(), d.String())
}

// Restating a device-provided object must not read as a second claim on its name.
func TestCommitCheckBaselineObjectRestatedIsNotCollision(t *testing.T) {
	d := diag.New()
	s := aclNamespaceSchema()
	baseline := parse.Parse(s, "ip access-list L\n", parse.Reject, d)
	cfg := parse.Parse(s, "ip access-list L\n", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, baseline, d)
	assert.False(t, d.HasErrors(), d.String())
}

// A composite exclusive name renders every component, not just the first.
func TestCommitCheckCompositeNameRendersEveryComponent(t *testing.T) {
	s := schema.New()
	s.Node("ip acl {{ name:word }} seq {{ seq:uint }}").Card(schema.ZeroToN).
		Kind("ip-acl").Tag("acl").Key("name", "seq").Namespace("acl")
	s.Node("mac acl {{ name:word }} seq {{ seq:uint }}").Card(schema.ZeroToN).
		Kind("mac-acl").Tag("acl").Key("name", "seq").Namespace("acl")
	d := checkText(t, s, "ip acl L seq 10\nmac acl L seq 10\n")
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), `name "L,10" under label "acl"`)
}

// Members match exclusive args by position, so the arg names may differ.
func TestCommitCheckNamespaceMatchesUniqueArgsByPosition(t *testing.T) {
	build := func() *schema.Schema {
		s := schema.New()
		s.Node("ip acl {{ name:word }} owner {{ own:word }}").
			Card(schema.ZeroToN).Kind("ip-acl").Tag("acl").
			Key("name", "own").Unique("name").Namespace("acl")
		s.Node("mac acl {{ id:word }} owner {{ own:word }}").
			Card(schema.ZeroToN).Kind("mac-acl").Tag("acl").
			Key("id", "own").Unique("id").Namespace("acl")
		return s
	}
	hit := checkText(t, build(), "ip acl L owner a\nmac acl L owner b\n")
	require.True(t, hit.HasErrors(), hit.String())
	assert.Contains(t, hit.String(), `name "L" under label "acl"`)
	miss := checkText(t, build(), "ip acl L owner a\nmac acl M owner b\n")
	assert.False(t, miss.HasErrors(), miss.String())
}
