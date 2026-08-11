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

// multiRefSchema has two distinct cross-references (vlan + route-map) so a
// single pass can surface more than one dangling-ref diagnostic.
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

// TestCommitCheckMultiKeyRefResolves guards the route-map fix: a definition with
// a composite (name, action, seq) key is still resolvable by a ref that targets
// the name alone, and multiple sequences under one name are not a duplicate.
func TestCommitCheckMultiKeyRefResolves(t *testing.T) {
	d := diag.New()
	in := "route-map FOO permit 10\nroute-map FOO permit 20\n" +
		"router bgp 65000\n  neighbor 1.1.1.1 route-map FOO in\n"
	cfg := parse.Parse(multiRefSchema(), in, diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

// TestCommitCheckMalformedListReportsInvalidList guards the standalone-entry
// contract: a ref on a malformed list arg reports the invalid list itself
// (CommitCheck may run on trees Phase A never saw) and no bogus per-item
// "does not exist" errors.
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

// requiresSchema requires an unkeyed "feature-bgp" instance before "router bgp".
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
	// The prerequisite is existential, not mandatory: no router bgp, no need
	// for the feature.
	d := diag.New()
	cfg := parse.Parse(requiresSchema(), "", diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
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
