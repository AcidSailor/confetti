package validate

import (
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
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
	ident.Name
	node   *schema.Node
	holder holder
	// owner is the structural parent, not holder.scope.Owner: a wider space still has narrower objects in it.
	owner string
}

// claimOf returns the exclusive name a node claims, or false when it claims none.
func (c relationChecker) claimOf(
	n *schema.Node,
	fromBaseline bool,
) (claim, bool) {
	// Rejecting a valid configuration is worse than missing a collision.
	name, ok, err := ident.ExclusiveName(n, ident.PerOwner)
	if err != nil {
		c.warnUnresolvable(n, fromBaseline, err)
		return claim{}, false
	}
	if !ok {
		return claim{}, false
	}
	return claim{
		Name:   name,
		node:   n,
		holder: holder{scope: name.Scope, key: name.Key},
		owner:  ident.OwnerKey(n),
	}, true
}

// warnUnresolvable reports a List the checker could not resolve, naming the baseline when the object came from there.
func (c relationChecker) warnUnresolvable(
	n *schema.Node,
	fromBaseline bool,
	err error,
) {
	raw := n.Fields[n.Def.ListSpec.Arg]
	if fromBaseline {
		// Baseline positions are not the caller's lines, so the report carries none.
		c.d.Add(
			diag.Warning,
			"baseline %s: unresolvable list %q: exclusive-name checking for this object skipped (%v)",
			n.Path(),
			raw,
			err,
		)
		return
	}
	c.d.AddAt(
		n.Line,
		diag.Warning,
		"%s: unresolvable list %q: exclusive-name checking for this line skipped (%v)",
		n.Path(),
		raw,
		err,
	)
}

// sameObject reports whether two claims restate one object rather than two claimants.
func sameObject(a, b claim) bool {
	// A restatement sits at the same position; a wider space does not merge two positions.
	// Unique narrows the name, so the full key still separates two objects sharing one.
	return a.node == b.node ||
		a.node.Def == b.node.Def && a.owner == b.owner &&
			ident.KeyValue(a.node) == ident.KeyValue(b.node)
}

// conflicts reports whether two claims contest one name; lists contest only where their members overlap.
func conflicts(a, b claim) bool {
	if a.IsList && b.IsList {
		return a.Members.Intersects(b.Members)
	}
	// ValidateRelations rejects a shared space whose members mix list and scalar names.
	return !a.IsList && !b.IsList
}

// checkExclusive reports every earlier object whose exclusive name this node also claims.
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
		// One list can overlap several disjoint holders, and each is its own fix.
		if !sameObject(first, cl) && conflicts(first, cl) {
			c.reportCollision(cl, first)
		}
	}
}

// reportCollision names the contested value, its scope, and the object that holds it.
func (c relationChecker) reportCollision(cl, first claim) {
	n, name := cl.node, strings.ReplaceAll(cl.Display, "\x00", ",")
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
