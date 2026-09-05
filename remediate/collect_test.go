package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
)

// collectOps runs collect over two configs parsed against testSchema.
func collectOps(t *testing.T, running, intended string) []op {
	t.Helper()
	s := testSchema()
	run, want := mustParse(t, s, running), mustParse(t, s, intended)
	d := diag.New()
	dv := &differ{order: buildOrderIndex(s), d: d}
	dv.collect(run.Root, want.Root, nil, nil)
	require.False(t, d.HasErrors(), d.String())
	return dv.ops
}

func TestCollectActionsAndSections(t *testing.T) {
	ops := collectOps(
		t,
		"vlan 10\ninterface Ethernet1/1\n  description A\n  shutdown\n",
		"interface Ethernet1/1\n  description B\n  ip address 10.0.0.1 255.255.255.0 secondary\n",
	)
	byText := map[string]op{}
	for _, o := range ops {
		byText[o.node.Text] = o
	}
	require.Len(t, ops, 4)

	assert.Equal(t, graph.Remove, byText["no vlan 10"].action)
	assert.Empty(t, byText["no vlan 10"].secs)

	assert.Equal(t, graph.Remove, byText["no shutdown"].action)
	require.Len(t, byText["no shutdown"].secs, 1)
	assert.Equal(
		t,
		"interface Ethernet1/1",
		byText["no shutdown"].secs[0].Text,
	)

	mod := byText["description B"]
	assert.Equal(t, graph.Modify, mod.action)
	require.NotNil(t, mod.runSrc)
	assert.Equal(t, "description A", mod.runSrc.Text)

	add := byText["ip address 10.0.0.1 255.255.255.0 secondary"]
	assert.Equal(t, graph.Add, add.action)
	assert.Equal(t, "interface Ethernet1/1", add.secs[0].Text)
}

func TestCollectEmptyKeptSectionYieldsNoOps(t *testing.T) {
	ops := collectOps(t,
		"interface Ethernet1/1\n  description A\n",
		"interface Ethernet1/1\n  description A\n")
	assert.Empty(t, ops)
}

func TestPathKeyCompare(t *testing.T) {
	// creates ascend before removes descend, prefixes sort before extensions
	a := pathKey{{0, 1, 0}}
	b := pathKey{{0, 2, 0}}
	c := pathKey{{1, -2, 0}} // remove of rank 2
	d := pathKey{{1, -1, 0}} // remove of rank 1: emits AFTER rank-2 remove
	e := pathKey{{0, 1, 0}, {0, 5, 0}}
	assert.Negative(t, a.compare(b))
	assert.Negative(t, b.compare(c))
	assert.Negative(t, c.compare(d))
	assert.Negative(t, a.compare(e)) // prefix first
	assert.Zero(t, a.compare(pathKey{{0, 1, 0}}))
}

func TestCollectBaselineKeysMatchDeclarationOrder(t *testing.T) {
	ops := collectOps(t, "", "vlan 10\ninterface Ethernet1/1\n  shutdown\n")
	require.Len(t, ops, 2) // added vlan subtree + added interface subtree
	// vlan is declared before interface => smaller key
	assert.Negative(t, ops[0].key.compare(ops[1].key))
	assert.Equal(t, "vlan 10", ops[0].node.Text)
}

func TestOpViewsAndPath(t *testing.T) {
	ops := collectOps(t,
		"interface Ethernet1/1\n  shutdown\n",
		"interface Ethernet1/1\n")
	require.Len(t, ops, 1)
	assert.Equal(t, "interface Ethernet1/1 / no shutdown", opPath(ops[0]))
	vs := opViews(ops)
	require.Len(t, vs, 1)
	assert.Equal(t, 0, vs[0].Index)
	assert.Equal(t, graph.Remove, vs[0].Action)
	assert.Equal(t, []string{"interface Ethernet1/1"}, vs[0].Path)
	assert.Equal(t, "no shutdown", vs[0].Text)
}
