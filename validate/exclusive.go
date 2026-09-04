package validate

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
)

// holder identifies one exclusive name: the space it lives in and the name itself.
type holder struct {
	scope ident.Scope
	// key is the exclusive arg values joined, with any List arg left blank.
	key string
}

// claim is one node's hold on an exclusive name.
type claim struct {
	holder  holder
	node    *schema.Node
	owner   string
	display string
	members listval.Members
	isList  bool
}

// claimOf returns the exclusive name a node claims, or false when it claims none.
func claimOf(n *schema.Node, d *diag.Diagnostics) (claim, bool) {
	def := n.Def
	if def == nil || len(def.KeyArgs) == 0 {
		return claim{}, false
	}
	args := def.ExclusiveArgs()
	listIdx := -1
	if def.ListSpec.Arg != "" {
		listIdx = slices.Index(args, def.ListSpec.Arg)
	}
	// Leave the list blank in the name so overlapping spellings share one holder.
	parts := make([]string, len(args))
	for i, a := range args {
		if i != listIdx {
			parts[i] = n.Fields[a]
		}
	}
	// Rejecting a valid configuration is worse than missing a collision.
	h := holder{
		scope: ident.ScopeOf(n, ident.PerOwner),
		key:   strings.Join(parts, "\x00"),
	}
	c := claim{holder: h, node: n, owner: ident.OwnerKey(n), display: h.key}
	if listIdx < 0 {
		return c, true
	}
	ls := def.ListSpec
	items, err := listval.Resolve(n.Fields[ls.Arg], ls.Sep, ls.Keywords())
	if err != nil {
		d.AddAt(
			n.Line,
			diag.Warning,
			"%s: unresolvable list %q: exclusive-name checking for this line skipped (%v)",
			n.Path(),
			n.Fields[ls.Arg],
			err,
		)
		return claim{}, false
	}
	parts[listIdx] = listval.Canonical(items, ls.Sep, ls.Keywords())
	c.display = strings.Join(parts, "\x00")
	c.members, c.isList = listval.Intervals(items), true
	return c, true
}

// sameObject reports whether two claims restate one object rather than two claimants.
func sameObject(a, b claim) bool {
	return a.node == b.node ||
		a.node.Def == b.node.Def && a.owner == b.owner &&
			ident.KeyValue(a.node) == ident.KeyValue(b.node)
}

// conflicts reports whether two claims contest one name; lists contest only where their members overlap.
func conflicts(a, b claim) bool {
	if a.isList && b.isList {
		return a.members.Intersects(b.members)
	}
	return !a.isList && !b.isList
}

// checkExclusive reports a node whose exclusive name another object already holds.
func (c relationChecker) checkExclusive(n *schema.Node) {
	cl, ok := c.claims[n]
	if !ok {
		return
	}
	for _, first := range c.held[cl.holder] {
		// Claims recorded after this node are not yet holders.
		if first.node == n {
			return
		}
		if sameObject(first, cl) || !conflicts(first, cl) {
			continue
		}
		c.reportCollision(cl, first)
		return
	}
}

// reportCollision names the contested value, its scope, and the object that holds it.
func (c relationChecker) reportCollision(cl, first claim) {
	n, name := cl.node, strings.ReplaceAll(cl.display, "\x00", ",")
	scope := cl.holder.scope
	if c.baseline[first.node] {
		c.d.AddAt(n.Line, diag.Error,
			"%s: name %q under %s is already held by baseline %q",
			n.Path(), name, scope, first.node.Text)
		return
	}
	c.d.AddAt(n.Line, diag.Error,
		"%s: name %q under %s is already held by %q (line %d)",
		n.Path(), name, scope, first.node.Text, first.node.Line)
}
