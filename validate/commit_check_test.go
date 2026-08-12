package validate

import (
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
		diag.Policy{Strict: true},
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
		diag.Policy{Strict: true},
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
	cfg := parse.Parse(multiRefSchema(), in, diag.Policy{Strict: true}, d)
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
	cfg := parse.Parse(multiRefSchema(), in, diag.Policy{Strict: true}, d)
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
	cfg := parse.Parse(s, "allowed vlan 10,,20\n", diag.Policy{Strict: true}, d)
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
		requiresSchema(), "router bgp 65000\n", diag.Policy{Strict: true}, d,
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
		diag.Policy{Strict: true},
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRequiresAbsentBothIsFine(t *testing.T) {
	// Requires is existential, not mandatory.
	d := diag.New()
	cfg := parse.Parse(requiresSchema(), "", diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

// l2l3Schema models the NX-OS L2/L3 split from issue #9: both sets on one header, exclusion by tag.
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
	cfg := parse.Parse(l2l3Schema(), in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	// The diagnostic names both offending lines and the tag in conflict.
	assert.Contains(
		t, d.String(), `mutually exclusive with "switchport" (line 2)`,
	)
	assert.Contains(t, d.String(), `via tag "l2"`)
	assert.Contains(t, d.String(), `via tag "l3"`)
}

func TestCommitCheckExcludeTagSameSetCoexists(t *testing.T) {
	// Many-to-many: members of one set coexist freely.
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport\n  switchport access vlan 20\n"
	cfg := parse.Parse(l2l3Schema(), in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckExcludeTagIgnoresGrandchildren(t *testing.T) {
	// Sibling scope stops at direct children: an l2-tagged grandchild is not in conflict.
	d := diag.New()
	in := "interface Ethernet1/1\n  ip address 10.0.0.1/24\n" +
		"  hsrp 1\n    ip 10.0.0.254\n"
	cfg := parse.Parse(l2l3Schema(), in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckExcludeTagScopedToParentInstance(t *testing.T) {
	// Exclusion is per parent instance: an l2 port and an l3 port coexist.
	d := diag.New()
	in := "interface Ethernet1/1\n  switchport\n" +
		"interface Ethernet1/2\n  ip address 10.0.0.1/24\n"
	cfg := parse.Parse(l2l3Schema(), in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRequiresSatisfiedByTag(t *testing.T) {
	// A Tag provides the same label presence a Kind does.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("feature lacp").Card(schema.ZeroToOne).Tag("feature-lacp")
	s.Node("port-channel {{ id:uint }}").
		Card(schema.ZeroToN).Key("id").Requires("feature-lacp")
	d := diag.New()
	cfg := parse.Parse(
		s, "feature lacp\nport-channel 10\n", diag.Policy{Strict: true}, d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestCommitCheckRefResolvesThroughTag(t *testing.T) {
	// A keyed definition's Tag also indexes its key values.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").
		Card(schema.ZeroToN).Kind("vlan").Tag("bridge").Key("id")
	s.Node("member {{ vlan:vlan }}").
		Card(schema.ZeroToN).Key("vlan").Ref("vlan", "bridge.id")
	d := diag.New()
	cfg := parse.Parse(
		s, "vlan 10\nmember 10\nmember 20\n", diag.Policy{Strict: true}, d,
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
		diag.Policy{Strict: true},
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Equal(t, 2, d.Items[0].Line, "points at the referring line")
}
