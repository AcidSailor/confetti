package ident

import (
	"strings"

	"github.com/acidsailor/confetti/schema"
)

// Category selects a node-pairing rule.
type Category int

const (
	Unmatched        Category = iota // No matched definition.
	Keyed                            // One or more key arguments.
	KindedSingle                     // A Kind identifies one unkeyed slot.
	IdempotentSingle                 // The definition identifies one reissuable slot.
	FullLine                         // The definition and full line identify the node.
)

// CategoryOf selects a pairing rule from the node definition.
func CategoryOf(n *schema.Node) Category {
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
func SingleOccupancy(def *schema.Def) bool {
	return def.Cardinality == schema.ZeroToOne || def.Cardinality == schema.One
}

// SlotDef reports whether a definition has one negatable, unkeyed slot per level.
func SlotDef(def *schema.Def) bool {
	return def != nil && len(def.KeyArgs) == 0 &&
		!def.EmptyOnRemove && SingleOccupancy(def)
}

// KindedSingleDef reports whether a definition pairs by Kind alone.
func KindedSingleDef(def *schema.Def) bool {
	return SlotDef(def) && def.KindName != "" && len(def.ToggleGroup) == 0
}

// Ident is a node's pairing identity.
type Ident struct {
	Def  *schema.Def
	Kind string
	Key  string
}

// Of computes a node's pairing identity.
func Of(n *schema.Node) Ident {
	switch CategoryOf(n) {
	case Unmatched:
		return Ident{Key: n.Text}
	case Keyed:
		if k := n.Def.KindName; k != "" {
			return Ident{Kind: k, Key: KeyValue(n)}
		}
		return Ident{Def: n.Def, Key: KeyValue(n)}
	case KindedSingle:
		return Ident{Kind: n.Def.KindName}
	case IdempotentSingle:
		// Exclude the value so instances of the same slot pair.
		return Ident{Def: n.Def}
	default: // FullLine
		return Ident{Def: n.Def, Key: n.Text}
	}
}

// KeyValue joins a keyed node's key arguments for pairing and duplicate detection; an unmatched node has no key.
func KeyValue(n *schema.Node) string {
	if n == nil || n.Def == nil {
		return ""
	}
	keys := n.Def.KeyArgs
	parts := make([]string, len(keys))
	for i, a := range keys {
		parts[i] = n.Fields[a]
	}
	return strings.Join(parts, "\x00")
}

// IsSection reports whether a node's schema definition declares children.
func IsSection(n *schema.Node) bool {
	return n.Def != nil && len(n.Def.Children) > 0
}
