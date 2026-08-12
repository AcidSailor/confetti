package ident

import (
	"strings"

	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// Category classifies a node for pairing purposes.
type Category int

const (
	Unmatched        Category = iota // def == nil
	Keyed                            // len(KeyArgs()) > 0
	KindedSingle                     // non-keyed, non-toggle, non-EmptyOnRemove, Kind set, single-occupancy
	IdempotentSingle                 // idempotent && card in {ZeroToOne, One}; KindedSingle wins when both apply
	FullLine                         // everything else
)

// CategoryOf classifies a tree node by its schema def.
func CategoryOf(n *tree.Node) Category {
	def := n.Def
	switch {
	case def == nil:
		return Unmatched
	case len(def.KeyArgs) > 0:
		return Keyed
	case KindedSingleDef(def):
		return KindedSingle
	case def.Idempotent && SingleOccupancy(def):
		return IdempotentSingle
	default:
		return FullLine
	}
}

// SingleOccupancy reports whether at most one instance of the definition can exist per level.
func SingleOccupancy(def *schema.Node) bool {
	return def.Cardinality == schema.ZeroToOne || def.Cardinality == schema.One
}

// SlotDef reports whether a definition holds one unkeyed slot per level; EmptyOnRemove sections are excluded because they have no header negation to pair with.
func SlotDef(def *schema.Node) bool {
	return def != nil && len(def.KeyArgs) == 0 &&
		!def.EmptyOnRemove && SingleOccupancy(def)
}

// KindedSingleDef reports whether a definition pairs by Kind alone; toggle members are excluded because they keep flip semantics.
func KindedSingleDef(def *schema.Node) bool {
	return SlotDef(def) && def.KindName != "" && len(def.ToggleGroup) == 0
}

// Ident pairs keyed nodes by Kind and key across sibling templates, Kind-marked single-occupancy nodes by Kind alone, and other nodes by definition; block bodies are excluded.
type Ident struct {
	Def  *schema.Node
	Kind string
	Key  string
}

// Of computes a node's pairing identity.
func Of(n *tree.Node) Ident {
	switch CategoryOf(n) {
	case Unmatched:
		return Ident{Key: n.Text}
	case Keyed:
		if k := n.Def.KindName; k != "" {
			return Ident{Kind: k, Key: KeyValue(n)}
		}
		return Ident{Def: n.Def, Key: KeyValue(n)}
	case KindedSingle:
		// Exclude the definition and value so sibling spellings of one Kind pair as one slot.
		return Ident{Kind: n.Def.KindName}
	case IdempotentSingle:
		// Exclude the value so instances of the same slot pair.
		return Ident{Def: n.Def}
	default: // FullLine
		return Ident{Def: n.Def, Key: n.Text}
	}
}

// KeyValue joins a keyed node's key arguments for pairing and duplicate detection.
func KeyValue(n *tree.Node) string {
	keys := n.Def.KeyArgs
	parts := make([]string, len(keys))
	for i, a := range keys {
		parts[i] = n.Fields[a]
	}
	return strings.Join(parts, "\x00")
}

// IsSection reports whether a node's schema definition declares children.
func IsSection(n *tree.Node) bool {
	return n.Def != nil && len(n.Def.Children) > 0
}
