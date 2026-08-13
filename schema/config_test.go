package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChildrenKeepSourceOrder(t *testing.T) {
	p := NewNode("parent")
	p.AddChild(NewNode("a"))
	p.AddChild(NewNode("b"))
	p.AddChild(NewNode("a")) // duplicate text is legal

	assert.Equal(t, 3, len(p.Children))
	assert.Equal(t, []string{"a", "b", "a"}, texts(p.Children))
}

func TestAddChildAndPath(t *testing.T) {
	s := New()
	cfg := NewConfig(s)
	iface := cfg.Root.AddChild(NewNode("interface Ethernet1/1"))
	ip := iface.AddChild(
		NewNode("ip address 10.0.0.1 255.255.255.0"),
	)

	assert.Equal(
		t,
		"interface Ethernet1/1 / ip address 10.0.0.1 255.255.255.0",
		ip.Path(),
	)
	assert.Equal(t, []*Node{iface}, cfg.Root.Children)
}

func TestAddChildRejectsSecondParent(t *testing.T) {
	a := NewNode("interface Ethernet1/1")
	b := NewNode("interface Ethernet1/2")
	child := NewNode("shutdown")
	a.AddChild(child)
	assert.Panics(t, func() { b.AddChild(child) })
}

func TestSetTextRetitlesInPlace(t *testing.T) {
	parent := NewNode("interface Ethernet1/1")
	child := parent.AddChild(NewNode("old"))
	child.Text = "new"

	assert.Equal(t, []string{"new"}, texts(parent.Children))
	// parent has no parent of its own, so it acts as the sentinel root.
	assert.Equal(t, "new", child.Path())
}

func TestWalkVisitsAll(t *testing.T) {
	s := New()
	cfg := NewConfig(s)
	iface := cfg.Root.AddChild(NewNode("interface Ethernet1/1"))
	iface.AddChild(NewNode("shutdown"))

	var seen []string
	Walk(cfg, func(n *Node) { seen = append(seen, n.Text) })
	assert.Equal(
		t,
		[]string{"interface Ethernet1/1", "shutdown"},
		seen,
	)
}

func TestNodeWalkCoversSubtreeOnly(t *testing.T) {
	root := NewNode("root")
	iface := root.AddChild(NewNode("interface Ethernet1/1"))
	iface.AddChild(NewNode("shutdown"))
	root.AddChild(NewNode("vlan 10"))

	var seen []string
	iface.Walk(func(n *Node) { seen = append(seen, n.Text) })
	assert.Equal(t, []string{"interface Ethernet1/1", "shutdown"}, seen)
}

func texts(ns []*Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Text
	}
	return out
}

func TestOpZeroValueIsNone(t *testing.T) {
	n := NewNode("interface Ethernet1/1")
	assert.Equal(t, OpNone, n.Op, "a fresh node must be untagged")
	assert.Equal(t, "none", OpNone.String())
}

func TestOpSetGet(t *testing.T) {
	n := NewNode("shutdown")
	n.Op = OpRemove
	assert.Equal(t, OpRemove, n.Op)
	assert.Equal(t, "remove", n.Op.String())
}

func TestOpStringAll(t *testing.T) {
	assert.Equal(t, "section", OpSection.String())
	assert.Equal(t, "add", OpAdd.String())
	assert.Equal(t, "modify", OpModify.String())
}

func TestOpStringUnknownIsVisible(t *testing.T) {
	// An out-of-range op must render as a visible debug form, not silently
	// masquerade as "none".
	assert.Equal(t, "Op(99)", Op(99).String())
}

func TestNodeBlockBody(t *testing.T) {
	n := NewNode("banner motd ^")
	assert.Nil(t, n.Block)
	n.Block = []string{"hello", "", "  indented world"}
	assert.Equal(t, []string{"hello", "", "  indented world"}, n.Block)
}

func TestReplaceChild(t *testing.T) {
	p := NewNode("parent")
	a := p.AddChild(NewNode("a"))
	b := p.AddChild(NewNode("b"))
	c := p.AddChild(NewNode("c"))

	r1, r2 := NewNode("r1"), NewNode("r2")
	p.ReplaceChild(b, r1, r2)

	assert.Equal(t, []string{"a", "r1", "r2", "c"}, texts(p.Children))
	assert.Same(t, p, r1.Parent)
	assert.Same(t, p, r2.Parent)
	assert.Nil(t, b.Parent)

	// Empty replacement = removal; the in-place splice leaves no stale entry.
	p.ReplaceChild(a)
	assert.Equal(t, []string{"r1", "r2", "c"}, texts(p.Children))
	assert.Nil(t, a.Parent)

	// Rails: non-child old, already-parented replacement.
	assert.Panics(t, func() { p.ReplaceChild(b) })
	assert.Panics(t, func() { p.ReplaceChild(c, r1) })
}

func TestLineSetAndClonesDrop(t *testing.T) {
	n := NewNode("vlan 10")
	assert.Equal(t, 0, n.Line)
	n.Line = 7
	assert.Equal(t, 7, n.Line)
	// Clones live in built trees, not the imported text (the RealIndent rule).
	assert.Equal(t, 0, n.CloneValue().Line)
	dst := NewNode("x")
	dst.Line = 3
	dst.SetValueFrom(n)
	assert.Equal(
		t,
		3,
		dst.Line,
		"SetValueFrom keeps position, replaces value",
	)
}
