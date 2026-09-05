package ident

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
)

// Name is the exclusive name one node claims in one name space.
type Name struct {
	// Scope is the name space the name lives in.
	Scope Scope
	// Key buckets the name; a List arg is left blank so overlapping spellings share one bucket.
	Key string
	// Display is Key with the List arg canonicalized, for diagnostics.
	Display string
	// Members holds the resolved List elements; set only when IsList.
	Members listval.Members
	// IsList reports whether a List arg forms part of the name.
	IsList bool
}

// ExclusiveName returns the exclusive name a node claims, false when it claims none, or an error when a List arg does not resolve.
func ExclusiveName(n *schema.Node, dflt Extent) (Name, bool, error) {
	// A name is held by the key, so a keyless or unmatched node holds nothing.
	if n == nil || n.Def == nil || len(n.Def.KeyArgs) == 0 {
		return Name{}, false, nil
	}
	def := n.Def
	args := def.ExclusiveArgs()
	listIdx := -1
	if def.ListSpec.Arg != "" {
		listIdx = slices.Index(args, def.ListSpec.Arg)
	}
	// Leave the list blank in the bucket key so overlapping spellings meet.
	parts := make([]string, len(args))
	for i, a := range args {
		if i != listIdx {
			parts[i] = n.Fields[a]
		}
	}
	name := Name{Scope: ScopeOf(n, dflt), Key: strings.Join(parts, "\x00")}
	name.Display = name.Key
	if listIdx < 0 {
		return name, true, nil
	}
	ls := def.ListSpec
	items, err := listval.Resolve(n.Fields[ls.Arg], ls.Sep, ls.Keywords())
	if err != nil {
		return Name{}, false, err
	}
	parts[listIdx] = listval.Canonical(items, ls.Sep, ls.Keywords())
	name.Display = strings.Join(parts, "\x00")
	name.Members, name.IsList = listval.Intervals(items), true
	return name, true, nil
}
