package ident

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
)

// Name identifies an exclusive resource claim.
type Name struct {
	Scope Scope
	// Key buckets the name; a List arg is left blank so overlapping spellings share one bucket.
	Key string
	// Display is the diagnostic form of Key with any List arg canonicalized.
	Display string
	// Members holds resolved List elements when IsList is true.
	Members listval.Members
	IsList  bool
}

// ExclusiveName returns the exclusive name a node claims, false when it claims none, or an error when a List arg does not resolve.
func ExclusiveName(n *schema.Node, dflt Extent) (Name, bool, error) {
	// Keyless and unmatched nodes hold no exclusive resource.
	if n == nil || n.Def == nil || len(n.Def.KeyArgs) == 0 {
		return Name{}, false, nil
	}
	def := n.Def
	args := def.ExclusiveArgs()
	listIdx := -1
	if def.ListSpec.Arg != "" {
		listIdx = slices.Index(args, def.ListSpec.Arg)
	}
	// Omitting the List arg places overlapping spellings in one bucket.
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
