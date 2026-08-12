package remediate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/tree"
)

// mkOp hand-builds an op for scheduler tests (no schema needed).
func mkOp(text string, secs []*tree.Node, key pathKey, act graph.Action) op {
	n := tree.NewNode(text)
	return op{node: n, src: n, action: act, secs: secs, key: key}
}

func sched(
	t *testing.T,
	ops []op,
	edges [][2]int,
	strict bool,
) ([]int, *diag.Diagnostics) {
	t.Helper()
	g := graph.New(opViews(ops))
	for _, e := range edges {
		g.AddEdge(e[0], e[1])
	}
	d := diag.New()
	dv := &differ{
		ops: ops,
		g:   g,
		why: map[[2]int]string{},
		p:   diag.Policy{Strict: strict},
		d:   d,
	}
	return dv.schedule(), d
}

func TestScheduleBaselineIsKeyOrder(t *testing.T) {
	ops := []op{
		mkOp("b", nil, pathKey{{0, 2, 1}}, graph.Add),
		mkOp("a", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("rm", nil, pathKey{{1, -3, 0}}, graph.Remove),
	}
	order, d := sched(t, ops, nil, true)
	require.False(t, d.HasErrors())
	assert.Equal(t, []int{1, 0, 2}, order)
}

func TestScheduleEdgeOverridesKey(t *testing.T) {
	ops := []op{
		mkOp("first-by-key", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("must-go-first", nil, pathKey{{0, 2, 1}}, graph.Add),
	}
	order, d := sched(t, ops, [][2]int{{1, 0}}, true)
	require.False(t, d.HasErrors())
	assert.Equal(t, []int{1, 0}, order)
}

func TestScheduleAffinityFinishesOpenSection(t *testing.T) {
	secA := []*tree.Node{tree.NewNode("section A")}
	secB := []*tree.Node{tree.NewNode("section B")}
	// After b1 unblocks a2, affinity must finish section B before returning to section A.
	ops := []op{
		mkOp("a1", secA, pathKey{{0, 1, 0}, {0, 1, 0}}, graph.Add),
		mkOp("b1", secB, pathKey{{0, 2, 1}, {0, 1, 0}}, graph.Add),
		mkOp("a2", secA, pathKey{{0, 1, 0}, {0, 2, 1}}, graph.Add),
		mkOp("b2", secB, pathKey{{0, 2, 1}, {0, 2, 1}}, graph.Add),
	}
	order, d := sched(t, ops, [][2]int{{1, 2}}, true)
	require.False(t, d.HasErrors())
	// a1; a2 blocked -> b1; b2 wins over a2 by affinity; then a2.
	assert.Equal(t, []int{0, 1, 3, 2}, order)
}

func TestScheduleCycleStrictErrors(t *testing.T) {
	ops := []op{
		mkOp("x", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("y", nil, pathKey{{0, 2, 1}}, graph.Add),
	}
	order, d := sched(t, ops, [][2]int{{0, 1}, {1, 0}}, true)
	assert.Nil(t, order)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "ordering cycle")
	assert.Contains(t, d.String(), "x")
	assert.Contains(t, d.String(), "y")
}

func TestScheduleCycleLenientBreaksDeterministically(t *testing.T) {
	ops := []op{
		mkOp("x", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("y", nil, pathKey{{0, 2, 1}}, graph.Add),
	}
	var first []int
	for range 5 {
		order, d := sched(t, ops, [][2]int{{0, 1}, {1, 0}}, false)
		require.NotNil(t, order)
		require.False(t, d.HasErrors())
		assert.True(t, strings.Contains(d.String(), "dropped ordering edge"))
		if first == nil {
			first = order
		}
		assert.Equal(t, first, order)
	}
	// Drop the greatest keyed edge so x to y remains and x emits first.
	assert.Equal(t, []int{0, 1}, first)
}

func TestScheduleCycleBreakTieBreaksOnToKey(t *testing.T) {
	// Resolve equal source keys with destination keys and drop the a-to-b edge deterministically.
	ops := []op{
		mkOp("a", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("b", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("c", nil, pathKey{{0, 0, 0}}, graph.Add),
	}
	var first []int
	for range 5 {
		order, d := sched(t, ops, [][2]int{{0, 1}, {1, 2}, {2, 0}}, false)
		require.NotNil(t, order)
		require.False(t, d.HasErrors())
		assert.Contains(t, d.String(), `dropped ordering edge "a" -> "b"`)
		if first == nil {
			first = order
		}
		assert.Equal(t, first, order)
	}
	// with a->b dropped, b frees first, then c, then a
	assert.Equal(t, []int{1, 2, 0}, first)
}

func TestScheduleCycleBreakNamesProtectedDependency(t *testing.T) {
	ops := []op{
		mkOp("x", nil, pathKey{{0, 1, 0}}, graph.Add),
		mkOp("y", nil, pathKey{{0, 2, 1}}, graph.Add),
	}
	g := graph.New(opViews(ops))
	g.AddEdge(0, 1)
	g.AddEdge(1, 0)
	// [1, 0] is the greatest-key edge and will be dropped.
	why := map[[2]int]string{{1, 0}: `ref vlan.id="10"`}
	d := diag.New()
	dv := &differ{ops: ops, g: g, why: why, d: d}
	order := dv.schedule()
	require.NotNil(t, order)
	assert.Contains(t, d.String(), `(protecting ref vlan.id="10")`)
}
