package alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareAlphaEndToEnd(t *testing.T) {
	// Same pair as TestRemediateAlphaEndToEnd; the view regroups the
	// scheduled log by path, so both root-level changes share one hunk.
	e := Engine()
	running, _ := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	intended, _ := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")
	out, d := e.Compare(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"+ vlan 20\n"+
			"- vlan 10\n"+
			"  interface Ethernet1/1\n"+
			"-   switchport access vlan 10\n"+
			"+   switchport access vlan 20\n"+
			"-   shutdown\n"+
			"+   no shutdown\n",
		out)
}

func TestCompareAlphaTrunkVlanModifyPair(t *testing.T) {
	e := Engine()
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20,30-40\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,25,30-40\n")

	view, d := e.Compare(running, intended)
	require.False(t, d.HasErrors(), d.String())
	// The change log stays a state view (whole old/new lines) while the
	// remediation artifact is incremental.
	assert.Contains(t, view, "-   switchport trunk allowed vlan 10,20,30-40\n")
	assert.Contains(t, view, "+   switchport trunk allowed vlan 10,25,30-40\n")
}

func TestCompareAlphaVlanMembership(t *testing.T) {
	e := Engine()
	running, _ := e.Import("vlan 7\n")
	intended, _ := e.Import("vlan 7,9-10\n")

	out, d := e.Compare(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "+ vlan 9\n+ vlan 10\n", out)
}
