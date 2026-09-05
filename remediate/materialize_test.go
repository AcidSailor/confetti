package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

func TestMaterializeSharedAndReopenedSections(t *testing.T) {
	s := testSchema()
	// parse once so section nodes carry real defs (render needs them)
	cfg := mustParse(
		t,
		s,
		"interface Ethernet1/1\n  shutdown\ninterface Ethernet1/2\n  shutdown\n",
	)
	secA := []*schema.Node{cfg.Root.Children[0]}
	secB := []*schema.Node{cfg.Root.Children[1]}

	lineAdd := func(text string) *schema.Node {
		n := schema.NewNode(text)
		n.Op = schema.OpAdd
		return n
	}
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
	out := schema.NewConfig(s)
	materialize([]int{0, 1, 2}, mkOps(), out)
	assert.Equal(t,
		"interface Ethernet1/1\n  description one\n"+
			"interface Ethernet1/2\n  description two\n"+
			"interface Ethernet1/1\n  description three\n",
		render.Render(out))
	// consecutive same-section ops share one header
	out2 := schema.NewConfig(s)
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
	out := schema.NewConfig(s)
	materialize([]int{0}, ops, out)
	assert.Equal(t,
		"interface Ethernet1/1\n  description A\n",
		render.Render(out))
}

func TestMaterializeReplacePreRidesFirst(t *testing.T) {
	s := testSchema()
	cfg := mustParse(t, s, "interface Ethernet1/1\n  shutdown\n")
	pre := schema.NewNode("no thing old")
	pre.Op = schema.OpRemove
	add := schema.NewNode("thing new")
	add.Op = schema.OpAdd
	ops := []op{{
		node: add, pre: pre, src: cfg.Root.Children[0],
		action: graph.Replace, secs: []*schema.Node{cfg.Root.Children[0]},
	}}
	out := schema.NewConfig(s)
	materialize([]int{0}, ops, out)
	assert.Equal(t,
		"interface Ethernet1/1\n  no thing old\n  thing new\n",
		render.Render(out))
}
