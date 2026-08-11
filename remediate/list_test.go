package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/tree"
)

func TestDiffListDeltaMixed(t *testing.T) {
	out, res := remediation(t,
		"interface eth1\n  switchport trunk allowed vlan 10,20,30-40\n",
		"interface eth1\n  switchport trunk allowed vlan 10,25,30-40\n")
	// Remove-before-add (the replace-pair pre slot), one line each,
	// compressed subsets.
	assert.Equal(t,
		"interface eth1\n"+
			"  switchport trunk allowed vlan remove 20\n"+
			"  switchport trunk allowed vlan add 25\n",
		out)
	// Both delta leaves are OpModify text leaves (def==nil, like negations).
	var tagged int
	tree.Walk(res.Tree, func(n *tree.Node) {
		if n.Op == tree.OpModify {
			tagged++
			assert.Nil(t, n.Def)
		}
	})
	assert.Equal(t, 2, tagged)
}

func TestDiffListDeltaPureAdd(t *testing.T) {
	out, _ := remediation(t,
		"interface eth1\n  switchport trunk allowed vlan 10\n",
		"interface eth1\n  switchport trunk allowed vlan 10,20-22\n")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan add 20-22\n", out)
}

func TestDiffListDeltaPureRemove(t *testing.T) {
	out, _ := remediation(t,
		"interface eth1\n  switchport trunk allowed vlan 10,20-22\n",
		"interface eth1\n  switchport trunk allowed vlan 10\n")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan remove 20-22\n", out)
}

func TestDiffListSetEqualNoChurn(t *testing.T) {
	// Formatting-only difference: same set, different spelling.
	out, res := remediation(t,
		"interface eth1\n  switchport trunk allowed vlan 10,11,12\n",
		"interface eth1\n  switchport trunk allowed vlan 10-12\n")
	assert.True(t, res.Empty())
	assert.Equal(t, "", out)
}

func TestDiffListNoDeltaFormsFallsBackToModify(t *testing.T) {
	out, _ := remediation(t,
		"interface eth1\n  monitor vlan 10,20\n",
		"interface eth1\n  monitor vlan 10,30\n")
	// No ListDelta declared: today's whole-line idempotent modify.
	assert.Equal(t, "interface eth1\n  monitor vlan 10,30\n", out)
}

func TestDiffListMalformedFallsBackToModify(t *testing.T) {
	// "30-20" parses (word matches) but does not Expand; Diff must never be
	// worse than the whole-line modify it replaces. mustParse runs no PhaseA,
	// so the malformed value legitimately reaches Diff.
	out, _ := remediation(t,
		"interface eth1\n  switchport trunk allowed vlan 30-20\n",
		"interface eth1\n  switchport trunk allowed vlan 10\n")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 10\n", out)
}

func TestDiffListMalformedIntendedFallsBackToModify(t *testing.T) {
	// Use one whole-line modification when the intended list is malformed.
	out, res := remediation(t,
		"interface eth1\n  switchport trunk allowed vlan 10\n",
		"interface eth1\n  switchport trunk allowed vlan 30-20\n")
	assert.Equal(t,
		"interface eth1\n  switchport trunk allowed vlan 30-20\n", out)
	assert.NotContains(t, out, "vlan add")
	assert.NotContains(t, out, "vlan remove")
	require.Len(t, res.Changes, 1)
	assert.Equal(t, graph.Modify, res.Changes[0].Action)
}

func TestDiffListKeyedNonKeyArg(t *testing.T) {
	out, _ := remediation(t,
		"vlan-group storage vlans 1,2\n",
		"vlan-group storage vlans 1,3\n")
	assert.Equal(t,
		"vlan-group storage vlans remove 2\n"+
			"vlan-group storage vlans add 3\n",
		out)
}

func TestDiffListDeltaOnProtectedNodeAllowed(t *testing.T) {
	// A protected list can emit deltas because a delta is a value change, not a deletion.
	out, _ := remediation(t,
		"interface eth1\n  core vlans 10,20\n",
		"interface eth1\n  core vlans 10,30\n")
	assert.Equal(t,
		"interface eth1\n"+
			"  core vlans remove 20\n"+
			"  core vlans add 30\n",
		out)
}

func TestDiffListDeltaChangeLog(t *testing.T) {
	s := testSchema()
	running := mustParse(t, s,
		"interface eth1\n  switchport trunk allowed vlan 10,20\n")
	intended := mustParse(t, s,
		"interface eth1\n  switchport trunk allowed vlan 10,30\n")
	res, d := Diff(running, intended, diag.Policy{})
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, res.Changes, 1)
	c := res.Changes[0]
	assert.Equal(t, graph.Modify, c.Action)
	// The Modify change refers to nodes in both source trees.
	assert.Same(t,
		running.Root.Children[0].Children[0], c.Running)
	assert.Same(t,
		intended.Root.Children[0].Children[0], c.Intended)
	assert.Equal(t, []string{"interface eth1"}, c.Path)
}
