package validate

import (
	"maps"
	"regexp"
	"slices"
	"sync"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
	"github.com/acidsailor/confetti/value"
)

// ImportCheck validates node values, cardinality, duplicate keys, toggle exclusions, and required nodes within each level.
func ImportCheck(cfg *tree.Config, d *diag.Diagnostics) {
	reg := cfg.Schema.Registry
	// Capture names are fixed per definition, so sort them once instead of per node.
	argNames := map[*schema.Node][]string{}

	tree.Walk(cfg, func(n *tree.Node) {
		def := n.Def
		if def == nil {
			return
		}
		fields := n.Fields
		args, cached := argNames[def]
		if !cached {
			// Sorted args keep the diagnostic order deterministic.
			args = slices.Sorted(maps.Keys(fields))
			argNames[def] = args
		}
		for _, arg := range args {
			val := fields[arg]
			vt, _ := reg.Get(def.ArgType(arg))
			if vt.Check == nil {
				continue // An unregistered type has a nil Check; schema construction rejects unknown types.
			}
			if err := vt.Check(val); err != nil {
				d.AddAt(
					n.Line,
					diag.Error,
					"%s: invalid %s %q: %v",
					n.Path(), arg, val, err,
				)
			}
		}
		if ls := def.ListSpec; ls.Arg != "" {
			checkListArg(n, ls, reg, d)
		}
	})

	checkCardinality(cfg.Root, cfg.Schema.Roots, d)
}

// checkCardinality validates child cardinality at one level and then recurses.
func checkCardinality(
	parent *tree.Node,
	allowed []*schema.Node,
	d *diag.Diagnostics,
) {
	children := parent.Children
	if len(children) == 0 && len(allowed) == 0 {
		return
	}

	count := map[*schema.Node]int{}
	seenKey := map[ident.Ident]bool{}
	kindSeen := map[string]*tree.Node{}
	// Map each canonical toggle member to the first present group member.
	toggleSeen := map[*schema.Node]*tree.Node{}

	for _, c := range children {
		def := c.Def
		if def == nil {
			checkCardinality(c, nil, d)
			continue
		}
		count[def]++
		if len(def.ToggleGroup) > 0 {
			canon := def.ToggleCanonical()
			// The duplicate check below handles two nodes with the same definition.
			if first, seen := toggleSeen[canon]; !seen {
				toggleSeen[canon] = c
			} else if first.Def != def {
				d.AddAt(
					c.Line,
					diag.Error,
					"%s: mutually exclusive with %q (line %d)",
					c.Path(),
					first.Text,
					first.Line,
				)
			}
		}
		switch {
		case len(def.KeyArgs) > 0:
			// Reuse the shared pairing identity so duplicates match what remediate and merge pair.
			id := ident.Of(c)
			if seenKey[id] {
				d.AddAt(
					c.Line,
					diag.Error,
					"%s: duplicate key %q",
					c.Path(),
					id.Key,
				)
			}
			seenKey[id] = true
		case ident.SingleOccupancy(def):
			if count[def] > 1 {
				d.AddAt(
					c.Line,
					diag.Error,
					"%s: duplicate (only one allowed)",
					c.Path(),
				)
			}
			// Same-definition duplicates were reported above.
			if ident.KindedSingleDef(def) {
				first, ok := kindSeen[def.KindName]
				switch {
				case !ok:
					kindSeen[def.KindName] = c
				case first.Def != def:
					d.AddAt(
						c.Line,
						diag.Error,
						"%s: duplicate spelling of slot %q (line %d)",
						c.Path(),
						first.Text,
						first.Line,
					)
				}
			}
		}
		checkCardinality(c, def.Children, d)
	}

	for _, sn := range allowed {
		if sn.Cardinality != schema.One || len(sn.KeyArgs) > 0 ||
			count[sn] > 0 {
			continue
		}
		if ident.KindedSingleDef(sn) && kindSeen[sn.KindName] != nil {
			continue
		}
		d.Add(
			diag.Error,
			"%s: missing required %q",
			parentPath(parent), sn.Template,
		)
	}
}

// parentPath labels a parent node for diagnostics, naming the sentinel root.
func parentPath(n *tree.Node) string {
	if p := n.Path(); p != "" {
		return p
	}
	return "<root>"
}

// anchoredCache stores compiled element patterns across imports.
var anchoredCache sync.Map

func anchored(pattern string) *regexp.Regexp {
	if re, ok := anchoredCache.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	re, _ := anchoredCache.LoadOrStore(
		pattern, regexp.MustCompile(`^(?:`+pattern+`)$`),
	)
	return re.(*regexp.Regexp)
}

// listItems parses a list and reports malformed syntax.
func listItems(
	n *tree.Node,
	ls schema.ListStrategy,
	d *diag.Diagnostics,
) ([]string, bool) {
	raw := n.Fields[ls.Arg]
	items, err := listval.Parts(raw, ls.Sep, ls.Keywords())
	if err != nil {
		d.AddAt(
			n.Line,
			diag.Error,
			"%s: invalid %s %q: %v",
			n.Path(),
			ls.Arg,
			raw,
			err,
		)
		return nil, false
	}
	return items, true
}

// checkListArg validates list syntax and each explicitly written item against its anchored element type.
func checkListArg(
	n *tree.Node,
	ls schema.ListStrategy,
	reg *value.Registry,
	d *diag.Diagnostics,
) {
	items, ok := listItems(n, ls, d)
	if !ok {
		return
	}
	vt, ok := reg.Get(ls.Elem)
	if !ok {
		return // List rejects unregistered element types during schema construction.
	}
	re := anchored(vt.Pattern)
	for _, it := range items {
		if !re.MatchString(it) {
			d.AddAt(
				n.Line,
				diag.Error,
				"%s: invalid %s item %q: not a valid %s",
				n.Path(),
				ls.Arg,
				it,
				ls.Elem,
			)
			continue
		}
		if vt.Check != nil {
			if err := vt.Check(it); err != nil {
				d.AddAt(n.Line, diag.Error, "%s: invalid %s item %q: %v",
					n.Path(), ls.Arg, it, err)
			}
		}
	}
}
