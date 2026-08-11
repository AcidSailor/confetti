package validate

import (
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/tree"
)

// target identifies one indexed key value: the Kind, its key argument, and the value.
type target struct{ kind, arg, val string }

// CommitCheck validates references and prerequisites against the assembled tree.
func CommitCheck(cfg *tree.Config, d *diag.Diagnostics) {
	// Index every declared key value so refs can resolve against it.
	index := map[target]bool{}
	// Record whether each required Kind has any instance.
	present := map[string]bool{}

	tree.Walk(cfg, func(n *tree.Node) {
		def := n.Def
		if def == nil {
			return
		}
		kind := def.KindName
		if kind == "" {
			return
		}
		present[kind] = true
		for _, arg := range def.KeyArgs {
			index[target{kind, arg, n.Fields[arg]}] = true
		}
	})

	tree.Walk(cfg, func(n *tree.Node) {
		def := n.Def
		if def == nil {
			return
		}
		for _, k := range def.RequiresKinds {
			if !present[k] {
				d.AddAt(
					n.Line,
					diag.Error,
					"%s: requires a %s instance",
					n.Path(), k,
				)
			}
		}
		for _, ref := range def.Refs {
			vals := []string{n.Fields[ref.FromArg]}
			// Resolve each explicit list item and report malformed lists because CommitCheck can run without Phase A.
			if ls := def.ListSpec; ls.Arg == ref.FromArg {
				items, ok := listItems(n, ls, d)
				if !ok {
					continue
				}
				vals = items
			}
			for _, v := range vals {
				if !index[target{ref.TargetKind, ref.TargetKey, v}] {
					d.AddAt(
						n.Line,
						diag.Error,
						"%s: %s %q does not exist",
						n.Path(), ref.TargetKind, v,
					)
				}
			}
		}
	})
}
