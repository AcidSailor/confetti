package alpha

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/merge"
)

func TestAlphaMergeToggleConflict(t *testing.T) {
	// Merge must treat shutdown and no shutdown on one interface as a single conflicting slot.
	e := Engine()
	a, da := e.Import("interface Ethernet1/1\n  shutdown\n")
	require.False(t, da.HasErrors(), da.String())
	b, db := e.Import("interface Ethernet1/1\n  no shutdown\n")
	require.False(t, db.HasErrors(), db.String())
	_, d := e.Merge(merge.Options{Resolve: merge.Refuse}, a, b)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "conflicts with")
}

func TestAlphaMergeVlanMembershipUnion(t *testing.T) {
	// Merge canonical membership instances by key, deduplicate overlap, and retain children.
	e := Engine()
	p1, d1 := e.Import("vlan 1-2\n")
	require.False(t, d1.HasErrors(), d1.String())
	p2, d2 := e.Import("vlan 2,3\nvlan 3\n  name STORAGE\n")
	require.False(t, d2.HasErrors(), d2.String())

	merged, d := e.Merge(merge.Options{}, p1, p2)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(merged)
	assert.Equal(t, "vlan 1\nvlan 2\nvlan 3\n  name STORAGE\n", out)
}

func TestAlphaMergeTrunkKeywordUnion(t *testing.T) {
	e := Engine()
	p1, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan none\n")
	p2, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10\n")
	merged, d := e.Merge(merge.Options{}, p1, p2)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(merged)
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10\n", out)

	p3, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan all\n")
	merged2, d2 := e.Merge(merge.Options{}, p3, p2)
	require.False(t, d2.HasErrors(), d2.String())
	out2, _ := e.Render(merged2)
	// Union with the whole domain canonicalizes back to the keyword.
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan all\n", out2)
}
