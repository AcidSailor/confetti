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
	IdempotentSingle                 // idempotent && card in {ZeroToOne, One}
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
	case def.Idempotent &&
		(def.Cardinality == schema.ZeroToOne || def.Cardinality == schema.One):
		return IdempotentSingle
	default:
		return FullLine
	}
}

// Ident pairs keyed nodes by Kind and key across sibling templates, and other nodes by definition; block bodies are excluded.
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
