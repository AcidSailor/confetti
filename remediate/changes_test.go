package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

// findNode returns a source node for pointer-identity assertions.
func findNode(t *testing.T, cfg *schema.Config, text string) *schema.Node {
	t.Helper()
	var got *schema.Node
	schema.Walk(cfg, func(n *schema.Node) {
		if got == nil && n.Text == text {
			got = n
		}
	})
	require.NotNil(t, got, "node %q not found", text)
	return got
}

func TestChangesPairSourceNodes(t *testing.T) {
	s := testSchema()
	running := mustParse(t, s, "vlan 10\n"+
		"interface Ethernet1/1\n"+
		"  description A\n"+
		"  ip address 10.0.0.1 255.255.255.0 secondary\n")
	intended := mustParse(t, s, "vlan 10\n"+
		"vlan 99\n"+
		"interface Ethernet1/1\n"+
		"  description B\n")
	res, d := Diff(running, intended, Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())

	// vlan 10 and the interface header are kept: no Change for either.
	require.Len(t, res.Changes, 3)
	byText := map[string]Change{}
	for _, c := range res.Changes {
		n := c.Intended
		if n == nil {
			n = c.Running
		}
		byText[n.Text] = c
	}

	add := byText["vlan 99"]
	assert.Equal(t, graph.Add, add.Action)
	assert.Same(t, findNode(t, intended, "vlan 99"), add.Intended)
	assert.Nil(t, add.Running)
	assert.Empty(t, add.Path)

	mod := byText["description B"]
	assert.Equal(t, graph.Modify, mod.Action)
	assert.Same(t, findNode(t, intended, "description B"), mod.Intended)
	assert.Same(t, findNode(t, running, "description A"), mod.Running)
	assert.Equal(t, []string{"interface Ethernet1/1"}, mod.Path)

	rem := byText["ip address 10.0.0.1 255.255.255.0 secondary"]
	assert.Equal(t, graph.Remove, rem.Action)
	assert.Same(
		t,
		findNode(t, running, "ip address 10.0.0.1 255.255.255.0 secondary"),
		rem.Running,
	)
	assert.Nil(t, rem.Intended)
	assert.Equal(t, []string{"interface Ethernet1/1"}, rem.Path)
}

func TestChangesOrderMatchesArtifact(t *testing.T) {
	s := testSchema()
	running := mustParse(t, s,
		"vlan 50\ninterface Ethernet1/1\n  switchport access vlan 50\n")
	res, d := Diff(running, mustParse(t, s, ""), Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, res.Changes, 2)
	assert.Equal(t, graph.Remove, res.Changes[0].Action)
	assert.Equal(t, "interface Ethernet1/1", res.Changes[0].Running.Text)
	assert.Equal(t, graph.Remove, res.Changes[1].Action)
	assert.Equal(t, "vlan 50", res.Changes[1].Running.Text)
}

func TestChangesSectionAddIsOneChange(t *testing.T) {
	s := testSchema()
	intended := mustParse(t, s, "interface Ethernet1/1\n  description A\n")
	res, d := Diff(mustParse(t, s, ""), intended, Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, res.Changes, 1)
	c := res.Changes[0]
	assert.Equal(t, graph.Add, c.Action)
	assert.Same(t, findNode(t, intended, "interface Ethernet1/1"), c.Intended)
	kids := c.Intended.Children
	require.Len(t, kids, 1)
	assert.Equal(t, "description A", kids[0].Text)
}

func TestChangesReplaceIsOneEntry(t *testing.T) {
	// A keyed non-idempotent value change is one logical Replace change.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }} state {{ st:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	running := mustParse(t, s, "vlan 10 state enable\n")
	intended := mustParse(t, s, "vlan 10 state disable\n")
	res, d := Diff(running, intended, Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, res.Changes, 1)
	c := res.Changes[0]
	assert.Equal(t, graph.Replace, c.Action)
	assert.Same(t, findNode(t, running, "vlan 10 state enable"), c.Running)
	assert.Same(t, findNode(t, intended, "vlan 10 state disable"), c.Intended)
}

func TestChangesToggleFlipCarriesBothSides(t *testing.T) {
	s := testSchema()
	running := mustParse(t, s, "interface Ethernet1/1\n  shutdown\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  no shutdown\n")
	res, d := Diff(running, intended, Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, res.Changes, 1)
	c := res.Changes[0]
	assert.Equal(t, graph.Add, c.Action)
	assert.Same(t, findNode(t, intended, "no shutdown"), c.Intended)
	assert.Same(t, findNode(t, running, "shutdown"), c.Running)
	assert.Equal(t,
		"interface Ethernet1/1\n  no shutdown\n", render.Render(res.Tree))

	running2 := mustParse(t, s, "interface Ethernet1/1\n  no shutdown\n")
	intended2 := mustParse(t, s, "interface Ethernet1/1\n  shutdown\n")
	res2, d2 := Diff(running2, intended2, Options{Cycle: Break})
	require.False(t, d2.HasErrors(), d2.String())
	require.Len(t, res2.Changes, 1)
	assert.Same(
		t,
		findNode(t, running2, "no shutdown"),
		res2.Changes[0].Running,
	)
	assert.Same(t, findNode(t, intended2, "shutdown"), res2.Changes[0].Intended)
}

func TestChangesStrictCycleEmpty(t *testing.T) {
	// No log for an artifact we refused to emit.
	s := schema.New()
	s.Node("alpha {{ v:word }}").Card(schema.ZeroToN)
	s.Node("beta {{ v:word }}").Card(schema.ZeroToN)
	s.OrderHook(func(g *graph.Graph) {
		g.AddEdge(0, 1)
		g.AddEdge(1, 0)
	})
	res, d := Diff(
		mustParse(t, s, ""),
		mustParse(t, s, "alpha one\nbeta two\n"),
		Options{Cycle: Abort},
	)
	require.True(t, d.HasErrors())
	assert.Empty(t, res.Changes)
}
