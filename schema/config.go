package schema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Op identifies a node's remediation role; OpNone must remain the zero value for ordinary parsed nodes.
type Op int

const (
	OpNone    Op = iota // A normal node outside remediation.
	OpSection           // A retained section header with changed children.
	OpAdd               // A line present only in intended.
	OpRemove            // A line present only in running and rendered as negation.
	OpModify            // An idempotent slot rendered with its new value.
)

func (o Op) String() string {
	switch o {
	case OpNone:
		return "none"
	case OpSection:
		return "section"
	case OpAdd:
		return "add"
	case OpRemove:
		return "remove"
	case OpModify:
		return "modify"
	default:
		return fmt.Sprintf("Op(%d)", int(o))
	}
}

// Node stores one parsed configuration line and its children.
type Node struct {
	Text       string
	Def        *Def              // The matched definition, or nil when unknown.
	Fields     map[string]string // Captured argument values; live, not a copy.
	RealIndent int               // Raw parsed indentation column.
	Line       int               // A 1-based source line, or zero when absent.
	Op         Op                // The remediation operation.
	Block      []string          // Raw block body lines, or nil for ordinary nodes.

	// Parent and Children are maintained by AddChild, ReplaceChild, and InsertChildBefore.
	Parent   *Node
	Children []*Node
}

// NewNode creates an unmatched node with the given text.
func NewNode(text string) *Node {
	return &Node{Text: text}
}

// CloneValue deep-copies value state into an unparented node without children, operation, indentation, or source line.
func (n *Node) CloneValue() *Node {
	c := NewNode("")
	c.SetValueFrom(n)
	return c
}

// cloneTree deep-copies a subtree, including source position and operation, into an unparented node.
func (n *Node) cloneTree() *Node {
	c := n.CloneValue()
	c.RealIndent, c.Line, c.Op = n.RealIndent, n.Line, n.Op
	for _, ch := range n.Children {
		c.AddChild(ch.cloneTree())
	}
	return c
}

// SetValueFrom deep-copies src value state while preserving this node's tree position and children.
func (n *Node) SetValueFrom(src *Node) {
	n.Def = src.Def
	n.Fields = maps.Clone(src.Fields)
	n.Block = slices.Clone(src.Block)
	n.Text = src.Text
}

// SameValue reports whether both nodes carry the same definition, rendered line, and block body.
func (n *Node) SameValue(o *Node) bool {
	return n.Def == o.Def && n.Text == o.Text && slices.Equal(n.Block, o.Block)
}

// AddChild attaches c under n and panics if c already has a parent.
func (n *Node) AddChild(c *Node) *Node {
	if c.Parent != nil {
		panic("schema: AddChild on a node that already has a parent")
	}
	c.Parent = n
	n.Children = append(n.Children, c)
	return c
}

// ReplaceChild replaces old with zero or more parentless nodes.
func (n *Node) ReplaceChild(old *Node, repl ...*Node) {
	if old.Parent != n {
		panic("schema: ReplaceChild on a node that is not a child")
	}
	for _, c := range repl {
		if c.Parent != nil {
			panic("schema: ReplaceChild with a node that already has a parent")
		}
		c.Parent = n
	}
	old.Parent = nil
	n.Children = spliceChildren(n.Children, old, repl)
}

// InsertChildBefore inserts parentless c before child ref and retains ref.
func (n *Node) InsertChildBefore(ref, c *Node) {
	if ref.Parent != n {
		panic("schema: InsertChildBefore ref is not a child")
	}
	if c.Parent != nil {
		panic("schema: InsertChildBefore with a node that already has a parent")
	}
	c.Parent = n
	n.Children = spliceChildren(n.Children, ref, []*Node{c, ref})
}

// spliceChildren substitutes repl for old; slices.Concat reallocates so a held child slice stays valid across a splice.
func spliceChildren(list []*Node, old *Node, repl []*Node) []*Node {
	if i := slices.Index(list, old); i >= 0 {
		return slices.Concat(list[:i], repl, list[i+1:])
	}
	return list
}

// Path joins ancestor texts with " / ", excluding the sentinel root.
func (n *Node) Path() string {
	var parts []string
	for p := n; p != nil && p.Parent != nil; p = p.Parent {
		parts = append(parts, p.Text)
	}
	slices.Reverse(parts)
	return strings.Join(parts, " / ")
}

// Config contains a sentinel root and the schema used to parse it.
type Config struct {
	Schema *Schema
	Root   *Node
}

// NewConfig returns an empty Config bound to s.
func NewConfig(s *Schema) *Config {
	return &Config{Schema: s, Root: NewNode("")}
}

// CloneConfig deep-copies a configuration so a caller cannot mutate the original.
func CloneConfig(c *Config) *Config {
	out := NewConfig(c.Schema)
	for _, ch := range c.Root.Children {
		out.Root.AddChild(ch.cloneTree())
	}
	return out
}

// Walk visits every real node (excludes the sentinel root) in pre-order.
func Walk(c *Config, fn func(*Node)) {
	for _, n := range c.Root.Children {
		n.Walk(fn)
	}
}

// Walk visits n and its descendants in pre-order.
func (n *Node) Walk(fn func(*Node)) {
	fn(n)
	for _, c := range n.Children {
		c.Walk(fn)
	}
}
