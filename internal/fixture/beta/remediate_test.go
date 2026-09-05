package beta

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertBefore requires that first appears ahead of second in out.
func assertBefore(t *testing.T, out, first, second string) {
	t.Helper()
	assert.Less(t, strings.Index(out, first), strings.Index(out, second), out)
}

func TestVlanStateFlipIsModify(t *testing.T) {
	e := Engine()
	run, d1 := e.Import(fmt.Sprintf(vlanTmpl, "state enable"))
	require.False(t, d1.HasErrors())
	intd, d2 := e.Import(fmt.Sprintf(vlanTmpl, "state disable"))
	require.False(t, d2.HasErrors())

	res, d := e.Remediate(run, intd)
	require.False(t, d.HasErrors())
	// Emit one Modify inside the retained section.
	out, _ := e.Render(res.Tree)
	assert.Equal(t, "vlan database\n  vlan 10 bridge 1 state disable\n", out)

	back, bd := e.Rollback(run, intd)
	require.False(t, bd.HasErrors())
	backOut, _ := e.Render(back.Tree)
	assert.Equal(t, "vlan database\n  vlan 10 bridge 1 state enable\n", backOut)
}

func TestMaxPathsChangeIsModify(t *testing.T) {
	e := Engine()
	base := "router bgp 65000\n  address-family ipv4 unicast\n    max-paths ebgp %s\n  exit-address-family\n"
	run, _ := e.Import(fmt.Sprintf(base, "4"))
	intd, _ := e.Import(fmt.Sprintf(base, "8"))
	res, d := e.Remediate(run, intd)
	require.False(t, d.HasErrors())
	out, _ := e.Render(res.Tree)
	assert.Equal(
		t,
		"router bgp 65000\n  address-family ipv4 unicast\n    max-paths ebgp 8\n  exit-address-family\n",
		out,
	)
}

func TestBridgeTeardownOrdersRefsFirst(t *testing.T) {
	e := Engine()
	run, d1 := e.Import(bridged)
	require.False(t, d1.HasErrors())
	// Retain VLAN database and empty it with per-VLAN negations.
	intd, d2 := e.Import("vlan database\ninterface xe1\n  switchport\n")
	require.False(t, d2.HasErrors())

	res, d := e.Remediate(run, intd)
	require.False(t, d.HasErrors())
	out, _ := e.Render(res.Tree)
	// Remove referrers before their targets.
	assertBefore(t, out, "no bridge-group 1", "no bridge 1")
	assertBefore(t, out, "no vlan 10 bridge 1", "no bridge 1")
	assertBefore(t, out, "no switchport access vlan 10", "no bridge 1")
	assert.NotContains(t, out, "no vlan database")
}

func TestInversionGolden(t *testing.T) {
	e := Engine()
	run, _ := e.Import(canonical)
	intdText := strings.Replace(canonical, "state enable", "state disable", 1)
	intd, d := e.Import(intdText)
	require.False(t, d.HasErrors())

	fwd, fd := e.Remediate(run, intd)
	require.False(t, fd.HasErrors())
	back, bd := e.Rollback(run, intd)
	require.False(t, bd.HasErrors())
	fwdOut, _ := e.Render(fwd.Tree)
	backOut, _ := e.Render(back.Tree)
	assert.Contains(t, fwdOut, "state disable")
	assert.Contains(t, backOut, "state enable")
}

func TestVlanRenameIsModifyNotDelete(t *testing.T) {
	// Pair named and unnamed VLAN definitions and emit one reissue without a later removal.
	e := Engine()
	run, d1 := e.Import(fmt.Sprintf(vlanTmpl, "state enable"))
	require.False(t, d1.HasErrors())
	intd, d2 := e.Import(fmt.Sprintf(vlanTmpl, "name CORE state enable"))
	require.False(t, d2.HasErrors())

	res, d := e.Remediate(run, intd)
	require.False(t, d.HasErrors())
	out, _ := e.Render(res.Tree)
	assert.Equal(
		t,
		"vlan database\n  vlan 10 bridge 1 name CORE state enable\n",
		out,
	)
	assert.NotContains(t, out, "no vlan")
}

func TestVlanDatabaseDropEqualsEmptying(t *testing.T) {
	// EmptyOnRemove drops VLANs individually and orders referrer removals first.
	e := Engine()
	run, d1 := e.Import(bridged)
	require.False(t, d1.HasErrors())
	intd, d2 := e.Import("interface xe1\n  switchport\n")
	require.False(t, d2.HasErrors())

	res, d := e.Remediate(run, intd)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(res.Tree)
	assert.NotContains(t, out, "no vlan database")
	assert.Contains(t, out, "vlan database\n  no vlan 10 bridge 1\n")
	assertBefore(t, out, "no switchport access vlan 10", "no vlan 10 bridge 1")
	assertBefore(t, out, "no vlan 10 bridge 1", "no bridge 1")

	// An empty running stanza needs no output when intended drops it.
	runEmpty, _ := e.Import("vlan database\ninterface xe1\n  switchport\n")
	res2, d3 := e.Remediate(runEmpty, intd)
	require.False(t, d3.HasErrors(), d3.String())
	assert.True(t, res2.Empty())
}
