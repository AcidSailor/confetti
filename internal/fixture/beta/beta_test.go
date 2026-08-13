package beta

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const canonical = `bridge 1 protocol rstp vlan-bridge
vlan database
  vlan 10 bridge 1 state enable
  vlan 20 bridge 1 name servers state enable
route-map RM-OUT permit 10
interface xe1
  description uplink
  mtu 9216
  switchport
  bridge-group 1
  switchport mode access
  switchport access vlan 10
  no shutdown
interface vlan1.10
  ip address 10.0.10.1/24
  ip address 10.0.11.1/24 secondary
router bgp 4200000001
  bgp router-id 10.0.0.1
  neighbor 10.0.0.2 remote-as 65001
  address-family ipv4 unicast
    network 10.0.10.0/24
    neighbor 10.0.0.2 activate
    neighbor 10.0.0.2 route-map RM-OUT out
    max-paths ebgp 4
  exit-address-family
`

// vlanTmpl is the bridge and VLAN-database preamble with one %s slot for the VLAN attributes.
const vlanTmpl = "bridge 1 protocol rstp vlan-bridge\n" +
	"vlan database\n" +
	"  vlan 10 bridge 1 %s\n"

// bridged wires a bridge, a VLAN, and an interface that refers to both.
const bridged = "bridge 1 protocol rstp vlan-bridge\n" +
	"vlan database\n" +
	"  vlan 10 bridge 1 state enable\n" +
	"interface xe1\n" +
	"  switchport\n" +
	"  bridge-group 1\n" +
	"  switchport access vlan 10\n"

func TestRoundTrip(t *testing.T) {
	e := Engine()
	cfg, d := e.Import(canonical)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Equal(t, canonical, out)
}

func TestImportDropsNoise(t *testing.T) {
	e := Engine()
	_, d := e.Import(
		"!\nvlan database\n vlan 10 bridge 1 state enable\n!\nend\n",
	)
	// "!"/"end" dropped pre-parse; the bridge ref is a commit-check concern
	assert.False(t, d.HasErrors(), d.String())
}

func TestStrictRejectsUnknown(t *testing.T) {
	e := Engine()
	_, d := e.Import("feature bgp\n")
	assert.True(t, d.HasErrors()) // no alpha-style feature gating in beta
}

func TestCommitCheckRefs(t *testing.T) {
	e := Engine()
	for _, bad := range []string{
		"interface xe1\n  bridge-group 2\n",            // no bridge 2
		"interface xe1\n  switchport access vlan 30\n", // no vlan 30
		"router bgp 65000\n  address-family ipv4 unicast\n    neighbor 10.0.0.2 route-map NOPE out\n  exit-address-family\n",
	} {
		cfg, d := e.Import(bad)
		require.False(t, d.HasErrors(), d.String())
		assert.True(t, e.CommitCheck(cfg).HasErrors(), bad)
	}
	// jointly consistent config passes
	cfg, d := e.Import(canonical)
	require.False(t, d.HasErrors())
	assert.False(t, e.CommitCheck(cfg).HasErrors())
}

func TestValueRanges(t *testing.T) {
	e := Engine()
	for _, bad := range []string{
		"vlan database\n  vlan 1 bridge 1 state enable\n",   // vlan 1 implicit
		"vlan database\n  vlan 10 bridge 33 state enable\n", // bridge > 32
		"interface xe1\n  ip address 10.0.0.1 255.0.0.0\n",  // mask form
		"interface xe1\n  mtu 63\n",
		"router bgp 4294967296\n", // > 4-byte asn
	} {
		_, d := e.Import(bad)
		assert.True(t, d.HasErrors(), bad)
	}
	// 4-byte asn accepted (would fail alpha's 1..65535)
	_, d := e.Import("router bgp 4200000001\n")
	assert.False(t, d.HasErrors(), d.String())
}

func TestASNAsdotSpelling(t *testing.T) {
	e := Engine()
	for _, good := range []string{
		"router bgp 1.0\n", // = 65536
		"router bgp 65535.65535\n",
	} {
		_, d := e.Import(good)
		assert.False(t, d.HasErrors(), good+d.String())
	}
	for _, bad := range []string{
		"router bgp 0.1\n",     // asdot high part starts at 1
		"router bgp 1.65536\n", // low part overflows 16 bits
	} {
		_, d := e.Import(bad)
		assert.True(t, d.HasErrors(), bad)
	}
}

func TestVlanRangeFoldsToInstances(t *testing.T) {
	e := Engine()
	cfg, d := e.Import(
		"bridge 1 protocol rstp vlan-bridge\n" +
			"vlan database\n  vlan 2-4 bridge 1 state enable\n")
	require.False(t, d.HasErrors(), d.String())
	require.False(t, e.CommitCheck(cfg).HasErrors())
	out, _ := e.Render(cfg)
	assert.Equal(t,
		"bridge 1 protocol rstp vlan-bridge\n"+
			"vlan database\n"+
			"  vlan 2 bridge 1 state enable\n"+
			"  vlan 3 bridge 1 state enable\n"+
			"  vlan 4 bridge 1 state enable\n",
		out)
}

func TestVlanRangeSpellingIsNotDrift(t *testing.T) {
	// Range spelling vs explicit per-vlan spelling of the same content:
	// remediation must see zero drift.
	e := Engine()
	running, dr := e.Import(
		"bridge 1 protocol rstp vlan-bridge\n" +
			"vlan database\n" +
			"  vlan 2 bridge 1 state enable\n" +
			"  vlan 3 bridge 1 state enable\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import(
		"bridge 1 protocol rstp vlan-bridge\n" +
			"vlan database\n  vlan 2-3 bridge 1 state enable\n")
	require.False(t, di.HasErrors(), di.String())
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
}

func TestVlanRangeStateConflictIsDupKey(t *testing.T) {
	// ImportCheck reports conflicting states for one VLAN identity as a duplicate key.
	e := Engine()
	_, d := e.Import(
		"bridge 1 protocol rstp vlan-bridge\n" +
			"vlan database\n" +
			"  vlan 3 bridge 1 state disable\n" +
			"  vlan 2-4 bridge 1 state enable\n")
	assert.True(t, d.HasErrors(), "conflicting states must not import clean")
}

func TestTrunkAddLinesUnionOnImport(t *testing.T) {
	// Successive add lines accumulate on-device; import folds them into one
	// slot spelled as the canonical add-form line.
	e := Engine()
	cfg, d := e.Import(
		"interface xe1\n" +
			"  switchport trunk allowed vlan add 10\n" +
			"  switchport trunk allowed vlan add 20-22\n" +
			"  switchport trunk allowed vlan add 12\n")
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Equal(t,
		"interface xe1\n  switchport trunk allowed vlan add 10,12,20-22\n",
		out)
}

func TestTrunkRemediateEmitsDeltaForms(t *testing.T) {
	e := Engine()
	running, dr := e.Import(
		"interface xe1\n  switchport trunk allowed vlan add 10,20\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import(
		"interface xe1\n  switchport trunk allowed vlan add 10,30\n")
	require.False(t, di.HasErrors(), di.String())
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(res.Tree)
	assert.Equal(t,
		"interface xe1\n"+
			"  switchport trunk allowed vlan remove 20\n"+
			"  switchport trunk allowed vlan add 30\n",
		out)
}

func TestTrunkSpellingEquivalenceIsNotDrift(t *testing.T) {
	// Multi-line and folded single-line spellings of the same set: no drift.
	e := Engine()
	running, _ := e.Import(
		"interface xe1\n" +
			"  switchport trunk allowed vlan add 10\n" +
			"  switchport trunk allowed vlan add 20\n")
	intended, _ := e.Import(
		"interface xe1\n  switchport trunk allowed vlan add 10,20\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
}

func TestVrfGrammar(t *testing.T) {
	e := Engine()
	in := "ip vrf RED\n" +
		"interface xe1\n" +
		"  ip vrf forwarding RED\n" +
		"router bgp 65000\n" +
		"  address-family ipv4 vrf RED\n" +
		"    network 10.1.0.0/24\n" +
		"  exit-address-family\n"
	cfg, d := e.Import(in)
	require.False(t, d.HasErrors(), d.String())
	require.False(t, e.CommitCheck(cfg).HasErrors())
	out, _ := e.Render(cfg)
	assert.Equal(t, in, out) // exit token re-emitted; vrf AF binds its own def
}

func TestVrfDanglingRefs(t *testing.T) {
	e := Engine()
	for _, bad := range []string{
		"interface xe1\n  ip vrf forwarding BLUE\n",
		"router bgp 65000\n  address-family ipv4 vrf BLUE\n    network 10.1.0.0/24\n  exit-address-family\n",
	} {
		cfg, d := e.Import(bad)
		require.False(t, d.HasErrors(), d.String())
		cc := e.CommitCheck(cfg)
		assert.True(t, cc.HasErrors(), bad)
		assert.Contains(t, cc.String(), `vrf "BLUE" does not exist`)
	}
}

func TestTrunkWholeSlotRemovalUsesRemoveForm(t *testing.T) {
	// Dropping the slot entirely must spell the vendor's remove form, not
	// "no switchport trunk allowed vlan add ...".
	e := Engine()
	running, _ := e.Import(
		"interface xe1\n  switchport trunk allowed vlan add 10,20\n")
	intended, _ := e.Import("interface xe1\n  description x\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(res.Tree)
	assert.Contains(t, out, "switchport trunk allowed vlan remove 10,20")
	assert.NotContains(t, out, "no switchport")
}
