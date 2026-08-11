package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func testOps(n int) []Op {
	ops := make([]Op, n)
	for i := range ops {
		ops[i] = Op{Index: i, Action: Add, Text: "op"}
	}
	return ops
}

func TestGraphEdges(t *testing.T) {
	g := New(testOps(3))
	assert.Len(t, g.Ops(), 3)
	assert.False(t, g.HasEdge(0, 1))

	g.AddEdge(2, 0)
	g.AddEdge(2, 1)
	g.AddEdge(0, 1)
	assert.True(t, g.HasEdge(2, 0))
	assert.Equal(t, [][2]int{{0, 1}, {2, 0}, {2, 1}}, g.Edges())
	assert.Equal(t, []int{0, 1}, g.Succ(2))

	g.RemoveEdge(2, 0)
	assert.False(t, g.HasEdge(2, 0))
	assert.Equal(t, [][2]int{{0, 1}, {2, 1}}, g.Edges())

	g.AddEdge(2, 1) // duplicate: idempotent, not a second edge
	assert.Equal(t, [][2]int{{0, 1}, {2, 1}}, g.Edges())
}

func TestOpsReturnsCopy(t *testing.T) {
	input := testOps(2)
	input[0].Path = []string{"parent"}
	g := New(input)
	ops := g.Ops()
	ops[0].Text = "mutated"
	ops[0].Path[0] = "mutated"
	ops[0], ops[1] = ops[1], ops[0]
	assert.Equal(t, "op", g.Ops()[0].Text)
	assert.Equal(t, []string{"parent"}, g.Ops()[0].Path)
	assert.Equal(t, 0, g.Ops()[0].Index)
}

func TestGraphPanics(t *testing.T) {
	g := New(testOps(2))
	assert.Panics(t, func() { g.AddEdge(0, 0) }) // self-edge
	assert.Panics(t, func() { g.AddEdge(0, 5) }) // out of range
}

func TestActionString(t *testing.T) {
	assert.Equal(t, "add", Add.String())
	assert.Equal(t, "remove", Remove.String())
	assert.Equal(t, "modify", Modify.String())
	assert.Equal(t, "replace", Replace.String())
}
