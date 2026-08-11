package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/tree"
)

func TestMaterializeSharedAndReopenedSections(t *testing.T) {
	s := testSchema()
	// parse once so section nodes carry real defs (render needs them)
	cfg := mustParse(
		t,
		s,
		"interface Ethernet1/1\n  shutdown\ninterface Ethernet1/2\n  shutdown\n",
	)
	secA := []*tree.Node{cfg.Root.Children[0]}
	secB := []*tree.Node{cfg.Root.Children[1]}

	lineAdd := func(text string) *tree.Node {
		n := tree.NewNode(text)
		n.Op = tree.OpAdd
		return n
	}
	// op nodes are attached directly (not cloned), so each materialize call
	// needs its own freshly built ops.
	mkOps := func() []op {
		return []op{
			{
				node:   lineAdd("description one"),
				src:    cfg.Root.Children[0],
				action: graph.Add,
				secs:   secA,
			},
			{
				node:   lineAdd("description two"),
				src:    cfg.Root.Children[1],
				action: graph.Add,
				secs:   secB,
			},
			{
				node:   lineAdd("description three"),
				src:    cfg.Root.Children[0],
				action: graph.Add,
				secs:   secA,
			},
		}
	}
	out := tree.NewConfig(s)
	materialize([]int{0, 1, 2}, mkOps(), out)
	// Ethernet1/1 is entered, left for Ethernet1/2, then re-entered:
	// three section headers total, the first reused for nothing.
	assert.Equal(t,
		"interface Ethernet1/1\n  description one\n"+
			"interface Ethernet1/2\n  description two\n"+
			"interface Ethernet1/1\n  description three\n",
		render.Render(out))
	// consecutive same-section ops share one header
	out2 := tree.NewConfig(s)
	materialize([]int{0, 2, 1}, mkOps(), out2)
	assert.Equal(t,
		"interface Ethernet1/1\n  description one\n  description three\n"+
			"interface Ethernet1/2\n  description two\n",
		render.Render(out2))
	require.Equal(t, 2, len(out2.Root.Children))
}

func TestMaterializeAddedSubtreeKeepsChildren(t *testing.T) {
	s := testSchema()
	cfg := mustParse(t, s, "interface Ethernet1/1\n  description A\n")
	added := buildAdd(cfg.Root.Children[0]) // whole subtree as one op node
	ops := []op{{node: added, src: cfg.Root.Children[0], action: graph.Add}}
	out := tree.NewConfig(s)
	materialize([]int{0}, ops, out)
	assert.Equal(t,
		"interface Ethernet1/1\n  description A\n",
		render.Render(out))
}

func TestMaterializeReplacePreRidesFirst(t *testing.T) {
	s := testSchema()
	cfg := mustParse(t, s, "interface Ethernet1/1\n  shutdown\n")
	pre := tree.NewNode("no thing old")
	pre.Op = tree.OpRemove
	add := tree.NewNode("thing new")
	add.Op = tree.OpAdd
	ops := []op{{
		node: add, pre: pre, src: cfg.Root.Children[0],
		action: graph.Replace, secs: []*tree.Node{cfg.Root.Children[0]},
	}}
	out := tree.NewConfig(s)
	materialize([]int{0}, ops, out)
	assert.Equal(t,
		"interface Ethernet1/1\n  no thing old\n  thing new\n",
		render.Render(out))
}
