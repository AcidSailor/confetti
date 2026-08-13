package remediate

import (
	"github.com/acidsailor/confetti/internal/lcp"
	"github.com/acidsailor/confetti/schema"
)

// materialize builds the remediation tree in scheduled order and opens or reopens section context as required.
func materialize(order []int, ops []op, out *schema.Config) {
	// srcs holds the open source sections; outs holds their clones in the output tree.
	var srcs, outs []*schema.Node
	// parent returns the innermost open section, or the root when no section is open.
	parent := func() *schema.Node {
		if len(outs) > 0 {
			return outs[len(outs)-1]
		}
		return out.Root
	}
	for _, i := range order {
		o := ops[i]
		keep := lcp.Len(srcs, o.secs)
		srcs, outs = srcs[:keep], outs[:keep]
		for _, s := range o.secs[keep:] {
			sec := cloneNode(s, schema.OpSection)
			parent().AddChild(sec)
			srcs, outs = append(srcs, s), append(outs, sec)
		}
		if o.pre != nil {
			parent().AddChild(o.pre)
		}
		parent().AddChild(o.node)
	}
}
