package remediate

import (
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/tree"
)

// Change describes one logical operation with read-only source-node references and outermost-first section context.
type Change struct {
	Action   graph.Action // Add | Remove | Modify | Replace
	Running  *tree.Node   // node in the running tree; nil for a pure Add
	Intended *tree.Node   // node in the intended tree; nil for a Remove
	Path     []string
}

// changesFrom creates the public change log in artifact emission order.
func changesFrom(seq []int, ops []op) []Change {
	cs := make([]Change, 0, len(seq))
	for _, i := range seq {
		o := ops[i]
		c := Change{Action: o.action, Path: secTexts(o.secs)}
		if o.action == graph.Remove {
			c.Running = o.src
		} else {
			c.Intended = o.src
			c.Running = o.runSrc // Set the running node for Modify or Replace.
			if o.flipRun != nil {
				c.Running = o.flipRun // Use the superseded running toggle partner.
			}
		}
		cs = append(cs, c)
	}
	return cs
}
