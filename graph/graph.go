package graph

import (
	"fmt"
	"maps"
	"slices"
)

// Action classifies a pending remediation change.
type Action int

const (
	Add     Action = iota // Create a line or subtree absent from running.
	Remove                // Negate a line or section present only in running.
	Modify                // Reissue an idempotent slot with a new value.
	Replace               // Negate and add one paired non-idempotent leaf.
)

func (a Action) String() string {
	switch a {
	case Add:
		return "add"
	case Remove:
		return "remove"
	case Modify:
		return "modify"
	case Replace:
		return "replace"
	default:
		return fmt.Sprintf("Action(%d)", int(a))
	}
}

// Op is a read-only pending change; Text contains the emitted line or the add line for paired operations.
type Op struct {
	Index  int
	Action Action
	Path   []string // Kept ancestor section text from outermost to innermost.
	Text   string
	Kind   string
	Key    string // Multi-argument keys join with a NUL separator.
}

// Graph stores operations and deterministic "from emits before to" edges.
type Graph struct {
	ops  []Op
	succ []map[int]bool
}

// New creates a graph over ops with no edges.
func New(ops []Op) *Graph {
	return &Graph{ops: ops, succ: make([]map[int]bool, len(ops))}
}

// Ops copies operations and their paths so hooks can modify the result safely.
func (g *Graph) Ops() []Op {
	ops := slices.Clone(g.ops)
	for i := range ops {
		ops[i].Path = slices.Clone(ops[i].Path)
	}
	return ops
}

func (g *Graph) check(i int) {
	if i < 0 || i >= len(g.ops) {
		panic(
			fmt.Sprintf(
				"graph: op index %d out of range [0,%d)",
				i,
				len(g.ops),
			),
		)
	}
}

// AddEdge records an idempotent ordering edge and panics on invalid indexes or a self-edge.
func (g *Graph) AddEdge(from, to int) {
	g.check(from)
	g.check(to)
	if from == to {
		panic(fmt.Sprintf("graph: self-edge on op %d", from))
	}
	if g.succ[from] == nil {
		g.succ[from] = map[int]bool{}
	}
	g.succ[from][to] = true
}

// RemoveEdge deletes the edge if present.
func (g *Graph) RemoveEdge(from, to int) {
	g.check(from)
	g.check(to)
	delete(g.succ[from], to)
}

// HasEdge reports whether "from emits before to" is recorded.
func (g *Graph) HasEdge(from, to int) bool {
	g.check(from)
	g.check(to)
	return g.succ[from][to]
}

// Succ returns from's successors in ascending index order.
func (g *Graph) Succ(from int) []int {
	g.check(from)
	return slices.Sorted(maps.Keys(g.succ[from]))
}

// Edges returns every edge sorted by (from, to).
func (g *Graph) Edges() [][2]int {
	n := 0
	for _, s := range g.succ {
		n += len(s)
	}
	out := make([][2]int, 0, n)
	for from := range g.succ {
		for _, to := range g.Succ(from) {
			out = append(out, [2]int{from, to})
		}
	}
	return out
}
