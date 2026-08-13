package remediate

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/schema"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/lcp"
)

// schedule orders operations by dependencies, section affinity, and baseline key.
func (dv *differ) schedule() []int {
	ops := dv.ops
	n := len(ops)
	indeg := make([]int, n)
	for _, e := range dv.g.Edges() {
		indeg[e[1]]++
	}
	emitted := make([]bool, n)
	var stack []*schema.Node // section path of the last emitted op
	out := make([]int, 0, n)
	for len(out) < n {
		best, bestAff := -1, -1
		for i := range n {
			if emitted[i] || indeg[i] > 0 {
				continue
			}
			aff := lcp.Len(ops[i].secs, stack)
			if (best == -1) || (aff > bestAff) ||
				(aff == bestAff && ops[i].key.compare(ops[best].key) < 0) {
				best, bestAff = i, aff
			}
		}
		if best == -1 { // Every remaining operation is part of a cycle.
			if !dv.breakCycle(indeg, emitted) {
				return nil
			}
			continue
		}
		emitted[best] = true
		out = append(out, best)
		stack = ops[best].secs
		for _, to := range dv.g.Succ(best) {
			indeg[to]--
		}
	}
	return out
}

// breakCycle reports an Abort cycle or removes the greatest-key edge from a Break cycle.
func (dv *differ) breakCycle(indeg []int, emitted []bool) bool {
	ops := dv.ops
	cyc := findCycle(dv.g, indeg, emitted)
	names := make([]string, len(cyc)+1)
	for i, x := range cyc {
		names[i] = opPath(ops[x])
	}
	names[len(cyc)] = names[0] // Repeat the first operation to close the cycle.
	if dv.cycle == Abort {
		dv.d.Add(diag.Error, "ordering cycle: %s", strings.Join(names, " -> "))
		return false
	}
	// Drop the lexicographically greatest from-key and to-key pair.
	bi := 0
	for i := 1; i < len(cyc); i++ {
		if cmpCycleEdge(ops, cyc, i, bi) > 0 {
			bi = i
		}
	}
	u, v := cyc[bi], cyc[(bi+1)%len(cyc)]
	dv.g.RemoveEdge(u, v)
	indeg[v]--
	// Include the dependency reason when edge derivation recorded one.
	msg := "ordering cycle: %s; dropped ordering edge %q -> %q"
	args := []any{strings.Join(names, " -> "), opPath(ops[u]), opPath(ops[v])}
	if w, ok := dv.why[[2]int{u, v}]; ok {
		msg += " (protecting %s)"
		args = append(args, w)
	}
	dv.d.Add(diag.Warning, msg, args...)
	return true
}

// cmpCycleEdge compares two cycle edges by their from-key and to-key pairs.
func cmpCycleEdge(ops []op, cyc []int, i, j int) int {
	next := func(x int) int { return cyc[(x+1)%len(cyc)] }
	if c := ops[cyc[i]].key.compare(ops[cyc[j]].key); c != 0 {
		return c
	}
	return ops[next(i)].key.compare(ops[next(j)].key)
}

// findCycle returns a deterministic dependency cycle by following the lowest-index stalled predecessor from the lowest stalled index.
func findCycle(g *graph.Graph, indeg []int, emitted []bool) []int {
	stalled := func(i int) bool { return !emitted[i] && indeg[i] > 0 }
	pred := make([][]int, len(indeg))
	for _, e := range g.Edges() { // Sorted edges produce predecessor lists sorted by source index.
		if stalled(e[0]) && stalled(e[1]) {
			pred[e[1]] = append(pred[e[1]], e[0])
		}
	}
	start := -1
	for i := range indeg {
		if stalled(i) {
			start = i
			break
		}
	}
	seen := map[int]int{} // Map each node to its position in the walk.
	var walk []int
	cur := start
	for {
		if pos, ok := seen[cur]; ok {
			cyc := slices.Clone(walk[pos:])
			slices.Reverse(
				cyc,
			) // The predecessor walk runs against edge direction.
			return cyc
		}
		seen[cur] = len(walk)
		walk = append(walk, cur)
		cur = pred[cur][0]
	}
}
