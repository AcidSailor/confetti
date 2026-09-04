package ident

import (
	"fmt"
	"slices"
	"strings"

	"github.com/acidsailor/confetti/schema"
)

// Extent is how far an exclusive name reaches when the schema does not declare it.
type Extent int

const (
	// PerOwner gives each enclosing object its own name space.
	PerOwner Extent = iota
	// Device gives the whole configuration one name space.
	Device
)

// Scope identifies the name space one exclusive name lives in.
type Scope struct {
	// Label is the Namespace or Kind, or empty when the definition itself names the space.
	Label string
	// Def names the space when Label is empty.
	Def *schema.Def
	// Owner is the object that opens the space, or empty when the space is device-wide.
	Owner string
}

// String renders the name space for diagnostics.
func (s Scope) String() string {
	switch {
	case s.Label != "":
		return fmt.Sprintf("label %q", s.Label)
	case s.Def != nil:
		return fmt.Sprintf("definition %q", s.Def.Template)
	default:
		return "an unnamed scope"
	}
}

// ScopeOf returns the name space a node's exclusive name lives in.
//
// An undeclared extent is resolved with dflt, and the two callers choose
// opposite defaults on purpose. Ordering passes Device because a needless
// release-before-claim edge is harmless while a missing one emits an invalid
// plan. Validation passes PerOwner because rejecting a valid configuration is
// worse than missing a collision. ScopedBy or ScopedByDevice removes the
// guess, and both callers then agree exactly.
func ScopeOf(n *schema.Node, dflt Extent) Scope {
	// An unmatched node has no definition, so it lives in no name space.
	if n == nil || n.Def == nil {
		return Scope{}
	}
	def := n.Def
	s := Scope{Label: def.ExclusiveLabel()}
	if s.Label == "" {
		s.Def = def
	}
	switch {
	case def.ScopeAnchor != nil:
		s.Owner = pathKey(anchorOf(n, def.ScopeAnchor))
	case def.DeviceScoped || def.NamespaceLabel != "":
		s.Owner = ""
	case dflt == PerOwner:
		s.Owner = pathKey(n.Parent)
	}
	return s
}

// OwnerKey identifies the object enclosing n, independent of any declared scope, so two positions never read as one object.
func OwnerKey(n *schema.Node) string {
	if n == nil {
		return ""
	}
	return pathKey(n.Parent)
}

// anchorOf returns the nearest ancestor of n defined by anchor, or nil when the node sits outside every anchor.
func anchorOf(n *schema.Node, anchor *schema.Def) *schema.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Def == anchor {
			return p
		}
	}
	return nil
}

// pathKey renders the pairing identity of every node from the root down to n, or "" for the sentinel root.
func pathKey(n *schema.Node) string {
	var parts []string
	for p := n; p != nil && p.Parent != nil; p = p.Parent {
		id := Of(p)
		tmpl := ""
		if id.Def != nil {
			tmpl = id.Def.Template
		}
		parts = append(parts, id.Kind+"\x01"+tmpl+"\x01"+id.Key)
	}
	slices.Reverse(parts)
	return strings.Join(parts, "\x02")
}
