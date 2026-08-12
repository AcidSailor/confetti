package validate

import (
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// target identifies one indexed key value: the label, its key argument, and the value.
type target struct{ label, arg, val string }

// CommitCheck validates relations (references, prerequisites, exclusions) against the assembled tree.
func CommitCheck(cfg *tree.Config, d *diag.Diagnostics) {
	// Report unresolvable relations here because a declaration a config never instantiates still constrains nothing.
	if cfg.Schema != nil {
		cfg.Schema.ValidateRelations(d)
	}
	// Index every declared key value under each label so tree-scope relations resolve against it.
	index := map[target]bool{}
	// Record whether any instance carries each label.
	present := map[string]bool{}

	tree.Walk(cfg, func(n *tree.Node) {
		def := n.Def
		if def == nil {
			return
		}
		for _, label := range def.Labels() {
			present[label] = true
			for _, arg := range def.KeyArgs {
				index[target{label, arg, n.Fields[arg]}] = true
			}
		}
	})

	tree.Walk(cfg, func(n *tree.Node) {
		def := n.Def
		if def == nil {
			return
		}
		for _, rel := range def.Relations {
			vals, ok := relValues(n, rel, d)
			if !ok {
				continue
			}
			for _, v := range vals {
				checkRelation(cfg, n, rel, v, present, index, d)
			}
		}
	})
}

// relValues returns the values a relation must match: one presence sentinel or the capture's list-expanded values.
func relValues(
	n *tree.Node,
	rel schema.Relation,
	d *diag.Diagnostics,
) ([]string, bool) {
	if rel.FromArg == "" {
		return []string{""}, true
	}
	// Validate list syntax here because CommitCheck can run without ImportCheck.
	if ls := n.Def.ListSpec; ls.Arg == rel.FromArg {
		return listItems(n, ls, d)
	}
	return []string{n.Fields[rel.FromArg]}, true
}

// checkRelation reports a diagnostic when one relation value is unsatisfied or in conflict.
func checkRelation(
	cfg *tree.Config,
	n *tree.Node,
	rel schema.Relation,
	v string,
	present map[string]bool,
	index map[target]bool,
	d *diag.Diagnostics,
) {
	if rel.Want == schema.Present {
		if relationSatisfied(n, rel, v, present, index) {
			return
		}
		if rel.FromArg == "" {
			d.AddAt(
				n.Line,
				diag.Error,
				"%s: requires a %s instance",
				n.Path(), rel.Label,
			)
			return
		}
		d.AddAt(
			n.Line,
			diag.Error,
			"%s: %s %q does not exist",
			n.Path(), rel.Label, v,
		)
		return
	}
	if hit := findConflict(cfg, n, rel, v); hit != nil {
		d.AddAt(
			n.Line,
			diag.Error,
			"%s: mutually exclusive with %q (line %d) via label %q",
			n.Path(), hit.Text, hit.Line, rel.Label,
		)
	}
}

// relationSatisfied reports whether any node in scope provides the label and value.
func relationSatisfied(
	n *tree.Node,
	rel schema.Relation,
	v string,
	present map[string]bool,
	index map[target]bool,
) bool {
	if rel.Scope == schema.ScopeSiblings {
		return findSibling(n, rel, v) != nil
	}
	if rel.FromArg == "" {
		return present[rel.Label]
	}
	return index[target{rel.Label, rel.TargetKey, v}]
}

// findConflict returns the first node other than n that matches rel within its scope.
func findConflict(
	cfg *tree.Config,
	n *tree.Node,
	rel schema.Relation,
	v string,
) *tree.Node {
	if rel.Scope == schema.ScopeSiblings {
		return findSibling(n, rel, v)
	}
	var hit *tree.Node
	tree.Walk(cfg, func(x *tree.Node) {
		if hit == nil && relMatches(x, n, rel, v) {
			hit = x
		}
	})
	return hit
}

// findSibling returns the first direct sibling of n that matches rel, or nil.
func findSibling(n *tree.Node, rel schema.Relation, v string) *tree.Node {
	if n.Parent == nil {
		return nil
	}
	for _, x := range n.Parent.Children {
		if relMatches(x, n, rel, v) {
			return x
		}
	}
	return nil
}

// relMatches reports whether x is a node other than self carrying the relation's label and, when FromArg is set, key value v.
func relMatches(x, self *tree.Node, rel schema.Relation, v string) bool {
	return x != self && x.Def != nil && x.Def.HasLabel(rel.Label) &&
		(rel.FromArg == "" || x.Fields[rel.TargetKey] == v)
}
