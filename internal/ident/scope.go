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
	// The zero value is invalid.
	_ Extent = iota
	// PerOwner gives each enclosing object its own name space.
	PerOwner
	// Device gives the whole configuration one name space.
	Device
)

// Scope identifies an exclusive resource name space.
type Scope struct {
	// Label is the Namespace or Kind; an empty Label uses Def.
	Label string
	Def   *schema.Def
	// Owner is empty for device-wide scope.
	Owner string
}

// String renders the label of the name space for diagnostics; the owner is not shown.
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

// ScopeOf resolves n's exclusive scope, using dflt when its schema declares no extent.
func ScopeOf(n *schema.Node, dflt Extent) Scope {
	if dflt != PerOwner && dflt != Device {
		panic("ident: ScopeOf needs an explicit Extent")
	}
	if n == nil || n.Def == nil {
		return Scope{}
	}
	def := n.Def
	s := Scope{Label: def.ExclusiveLabel()}
	if s.Label == "" {
		s.Def = def
	}
	anchor, device := def.ScopeExtent()
	switch {
	case anchor != nil:
		// Outside every anchor the node falls back to the device-wide space.
		s.Owner = pathKey(anchorOf(n, anchor))
	case device:
		s.Owner = ""
	case dflt == PerOwner:
		s.Owner = pathKey(n.Parent)
	}
	return s
}

// OwnerKey identifies the object enclosing n without applying its declared scope.
func OwnerKey(n *schema.Node) string {
	if n == nil {
		return ""
	}
	return pathKey(n.Parent)
}

// OwnerPath renders an owner key for diagnostics; definitions appear as authored.
func OwnerPath(key string) string {
	if key == "" {
		return ""
	}
	parts := strings.Split(key, "\x02")
	for i, p := range parts {
		f := strings.Split(p, "\x01")
		if len(f) != 3 {
			continue
		}
		parts[i] = strings.TrimSpace(f[1] + " " + f[2])
	}
	return strings.Join(parts, " / ")
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
