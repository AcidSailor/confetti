package remediate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// resource identifies a definition, reference, or exclusive value by Kind, key argument, definition, and key.
type resource struct {
	kind string
	arg  string
	def  *schema.Node
	key  string
}

// heldResource adds list members and cycle-warning text to a resource.
type heldResource struct {
	resource
	members listval.Members
	display string
	isList  bool
}

// edgeReasons stores the first recorded reason for each derived edge so lenient cycle warnings can identify the affected dependency.
type edgeReasons map[[2]int]string

func (r edgeReasons) put(from, to int, why string) {
	k := [2]int{from, to}
	if _, ok := r[k]; !ok {
		r[k] = why
	}
}

// buildGraph derives ordering edges among planned operations and then runs schema OrderHooks in registration order.
func (dv *differ) buildGraph() {
	dv.g = graph.New(opViews(dv.ops))
	dv.why = edgeReasons{}
	dv.deriveSlotCleanupEdges()
	dv.deriveRefEdges()
	dv.deriveMoveEdges()
	dv.deriveRequireEdges()
	for _, h := range dv.intended.Schema.OrderHooks {
		h(dv.g)
	}
}

// definesOf returns one resource per Kind and key argument in a subtree so references can target one part of a composite key.
func definesOf(n *tree.Node) []resource {
	var out []resource
	n.Walk(func(x *tree.Node) {
		def := x.Def
		if def == nil || def.KindName == "" {
			return
		}
		// A keyless Kind still provides the presence required by Requires.
		if len(def.KeyArgs) == 0 {
			out = append(out, resource{kind: def.KindName})
			return
		}
		for _, a := range def.KeyArgs {
			out = append(
				out,
				resource{kind: def.KindName, arg: a, key: x.Fields[a]},
			)
		}
	})
	return out
}

// refsOf returns referenced resources in a subtree and expands semantic list values into separate references.
func refsOf(n *tree.Node, d *diag.Diagnostics) []resource {
	var out []resource
	n.Walk(func(x *tree.Node) {
		def := x.Def
		if def == nil {
			return
		}
		for _, r := range def.Refs {
			if ls := def.ListSpec; ls.Arg != "" && ls.Arg == r.FromArg {
				items, ok := resolveListArg(x, ls, d, "ref-ordering edges")
				if !ok {
					continue
				}
				for _, it := range items {
					out = append(out, resource{
						kind: r.TargetKind, arg: r.TargetKey, key: it,
					})
				}
				continue
			}
			out = append(
				out,
				resource{
					kind: r.TargetKind,
					arg:  r.TargetKey,
					key:  x.Fields[r.FromArg],
				},
			)
		}
	})
	return out
}

// addedSubtree returns the subtree an operation creates, or nil when it creates none; a Replace creates its intended half.
func addedSubtree(o op) *tree.Node {
	if o.action == graph.Add || o.action == graph.Replace {
		return o.src
	}
	return nil
}

// removedSubtree returns the subtree an operation deletes, including the old half of a Replace or cross-definition Modify.
func removedSubtree(o op) *tree.Node {
	switch o.action {
	case graph.Remove:
		return o.src
	case graph.Replace:
		return o.runSrc
	case graph.Modify:
		if o.runSrc != nil && o.src.Def != o.runSrc.Def {
			return o.runSrc
		}
	}
	return o.flipRun
}

// deriveSlotCleanupEdges orders extra stale spellings before the operation that reissues their Kind-paired slot.
func (dv *differ) deriveSlotCleanupEdges() {
	for i, rm := range dv.ops {
		if rm.action != graph.Remove ||
			ident.CategoryOf(rm.src) != ident.KindedSingle {
			continue
		}
		id := ident.Of(rm.src)
		for j, add := range dv.ops {
			if i == j || add.action == graph.Remove ||
				ident.CategoryOf(add.src) != ident.KindedSingle ||
				id != ident.Of(add.src) || !slices.Equal(rm.secs, add.secs) {
				continue
			}
			dv.addEdge(i, j, "single-occupancy slot cleanup")
		}
	}
}

// definerIndex maps each resource defined by the selected subtrees to the operation with the smallest baseline key.
func definerIndex(ops []op, pick func(op) *tree.Node) map[resource]int {
	idx := map[resource]int{}
	for i, o := range ops {
		src := pick(o)
		if src == nil {
			continue
		}
		for _, r := range definesOf(src) {
			if j, ok := idx[r]; !ok || ops[i].key.compare(ops[j].key) < 0 {
				idx[r] = i
			}
		}
	}
	return idx
}

// addEdge records an ordering edge together with the reason cycle warnings report.
func (dv *differ) addEdge(from, to int, reason string) {
	dv.g.AddEdge(from, to)
	dv.why.put(from, to, reason)
}

// deriveRefEdges orders definition adds before referrer adds and referrer removals before definition removals, including retargeting operations.
func (dv *differ) deriveRefEdges() {
	addDef := definerIndex(dv.ops, addedSubtree)
	rmDef := definerIndex(dv.ops, removedSubtree)
	for i, o := range dv.ops {
		if o.action != graph.Remove {
			for _, r := range refsOf(o.src, dv.d) {
				if j, ok := addDef[r]; ok && j != i {
					dv.addEdge(j, i, refReason(r))
				}
			}
		}
		// A Remove op deletes o.src; every other action can retarget away from runSrc or a superseded toggle partner.
		// Do not visit a Replace running subtree twice.
		rm := removedSubtree(o)
		if rm == o.runSrc {
			rm = nil
		}
		for _, src := range [2]*tree.Node{o.runSrc, rm} {
			if src == nil {
				continue
			}
			for _, r := range refsOf(src, dv.d) {
				if j, ok := rmDef[r]; ok && j != i {
					dv.addEdge(i, j, refReason(r))
				}
			}
		}
	}
}

// refReason renders a ref resource for the cycle-break warning.
func refReason(r resource) string {
	return fmt.Sprintf("ref %s.%s=%q", r.kind, r.arg, r.key)
}

// resourcesHeld returns comparable exclusive resources and warns about unresolvable lists.
func resourcesHeld(n *tree.Node, d *diag.Diagnostics) []heldResource {
	var out []heldResource
	n.Walk(func(x *tree.Node) {
		def := x.Def
		if def == nil || len(def.KeyArgs) == 0 {
			return
		}
		args := def.KeyArgs
		if u := def.UniqueArgs; len(u) > 0 {
			args = u
		}
		listIdx := -1
		if def.ListSpec.Arg != "" {
			listIdx = slices.Index(args, def.ListSpec.Arg)
		}
		// Exclude the list from the bucket key so overlapping spellings share a bucket.
		parts := make([]string, len(args))
		for i, a := range args {
			if i != listIdx {
				parts[i] = x.Fields[a]
			}
		}
		key := strings.Join(parts, "\x00")
		held := heldResource{resource: resourceFor(def, key), display: key}
		if listIdx >= 0 {
			ls := def.ListSpec
			items, ok := resolveListArg(x, ls, d, "exclusive-resource ordering")
			if !ok {
				return
			}
			parts[listIdx] = listval.Canonical(items, ls.Sep, ls.Keywords())
			held.members = listval.Intervals(items)
			held.display = strings.Join(parts, "\x00")
			held.isList = true
		}
		out = append(out, held)
	})
	return out
}

// resolveListArg resolves a list argument and warns when ordering must skip the line.
func resolveListArg(
	x *tree.Node,
	ls schema.ListStrategy,
	d *diag.Diagnostics,
	skipped string,
) ([]string, bool) {
	items, err := listval.Resolve(x.Fields[ls.Arg], ls.Sep, ls.Keywords())
	if err != nil {
		d.AddAt(
			x.Line,
			diag.Warning,
			"%s: unresolvable list %q: %s for this line skipped (%v)",
			x.Path(),
			x.Fields[ls.Arg],
			skipped,
			err,
		)
		return nil, false
	}
	return items, true
}

// resourceFor returns a Kind-keyed resource or uses the definition when Kind is empty.
func resourceFor(def *schema.Node, key string) resource {
	r := resource{kind: def.KindName, key: key}
	if r.kind == "" {
		r.def = def
	}
	return r
}

// deriveMoveEdges orders each resource release before a new claim on the same resource.
func (dv *differ) deriveMoveEdges() {
	type release struct {
		heldResource
		op int
	}
	freed := map[resource][]release{}
	for i, o := range dv.ops {
		src := removedSubtree(o)
		if src == nil {
			continue
		}
		for _, r := range resourcesHeld(src, dv.d) {
			freed[r.resource] = append(freed[r.resource], release{r, i})
		}
	}
	for i, o := range dv.ops {
		src := addedSubtree(o)
		if src == nil {
			continue
		}
		for _, r := range resourcesHeld(src, dv.d) {
			// Compare every release and claim in the bucket.
			for _, old := range freed[r.resource] {
				if old.op != i && old.conflicts(r) {
					dv.addEdge(old.op, i, moveReason(r))
				}
			}
		}
	}
}

// conflicts reports whether two held resources claim the same exclusive value.
func (a heldResource) conflicts(b heldResource) bool {
	if a.isList && b.isList {
		return a.members.Intersects(b.members)
	}
	return !a.isList && !b.isList
}

// moveReason formats an exclusive resource for a cycle warning.
func moveReason(h heldResource) string {
	name := h.kind
	if name == "" && h.def != nil {
		name = h.def.Template
	}
	return fmt.Sprintf("exclusive resource %s %q",
		name, strings.ReplaceAll(h.display, "\x00", ","))
}

// requirement is one Requires declaration found in an op's subtree.
type requirement struct {
	tmpl string // The owning definition template used in diagnostics.
	kind string
}

// requireReason renders a Requires prerequisite for the cycle-break warning.
func requireReason(rq requirement) string {
	return fmt.Sprintf("required kind %q", rq.kind)
}

func requirementsOf(n *tree.Node) []requirement {
	var out []requirement
	n.Walk(func(x *tree.Node) {
		def := x.Def
		if def == nil {
			return
		}
		for _, k := range def.RequiresKinds {
			out = append(out, requirement{tmpl: def.Template, kind: k})
		}
	})
	return out
}

// declaredKinds collects every Kind name any def declares, reusing the single schema enumeration in buildOrderIndex.
func declaredKinds(s *schema.Schema) map[string]bool {
	kinds := map[string]bool{}
	for n := range buildOrderIndex(s) {
		if n.KindName != "" {
			kinds[n.KindName] = true
		}
	}
	return kinds
}

// survivingKinds returns Kinds with the same complete identity in running and intended.
func survivingKinds(running, intended *tree.Config) map[string]bool {
	collect := func(c *tree.Config) map[resource]bool {
		set := map[resource]bool{}
		tree.Walk(c, func(n *tree.Node) {
			def := n.Def
			if def == nil || def.KindName == "" {
				return
			}
			// A keyless Kind survives when it is present in both configurations.
			set[resource{kind: def.KindName, key: ident.KeyValue(n)}] = true
		})
		return set
	}
	run, want := collect(running), collect(intended)
	out := map[string]bool{}
	for r := range run {
		if want[r] {
			out[r.kind] = true
		}
	}
	return out
}

// deriveRequireEdges orders Requires users after prerequisite adds and before prerequisite removals when no instance survives.
func (dv *differ) deriveRequireEdges() {
	ops, d := dv.ops, dv.d
	declared := declaredKinds(dv.intended.Schema)
	survivors := survivingKinds(dv.running, dv.intended)

	// Index the first add for each Kind by the smallest baseline key.
	addByKind := map[string]int{}
	for i, o := range ops {
		src := addedSubtree(o)
		if src == nil {
			continue
		}
		for _, r := range definesOf(src) {
			if j, ok := addByKind[r.kind]; !ok ||
				ops[i].key.compare(ops[j].key) < 0 {
				addByKind[r.kind] = i
			}
		}
	}

	// Index each removal once per Kind.
	removeByKind := map[string][]int{}
	for j, o := range ops {
		src := removedSubtree(o)
		if src == nil {
			continue
		}
		seen := map[string]bool{}
		for _, r := range definesOf(src) {
			if !seen[r.kind] {
				seen[r.kind] = true
				removeByKind[r.kind] = append(removeByKind[r.kind], j)
			}
		}
	}

	unsatSev := dv.p.Severity()
	reported := map[string]bool{}
	validRequirement := func(rq requirement) bool {
		if declared[rq.kind] {
			return true
		}
		once := rq.tmpl + "\x00" + rq.kind
		if !reported[once] {
			reported[once] = true
			d.Add(diag.Error, "%s: requires unknown kind %q", rq.tmpl, rq.kind)
		}
		return false
	}
	addRemovalEdges := func(i int, rq requirement) {
		for _, j := range removeByKind[rq.kind] {
			if j == i {
				continue
			}
			dv.addEdge(i, j, requireReason(rq))
		}
	}
	for i, o := range ops {
		// The removal walk covers prerequisites for Remove operations.
		if o.action != graph.Remove {
			for _, rq := range requirementsOf(o.src) {
				if !validRequirement(rq) || survivors[rq.kind] {
					continue
				}
				if j, ok := addByKind[rq.kind]; ok {
					if j != i {
						dv.addEdge(j, i, requireReason(rq))
					}
					continue
				}
				d.Add(
					unsatSev,
					"%s: requires a %q but the goal defines none",
					opPath(o), rq.kind,
				)
			}
		}
		// Remove prerequisites only after the operations that supersede their users.
		rm := removedSubtree(o)
		if rm == nil {
			continue
		}
		for _, rq := range requirementsOf(rm) {
			if validRequirement(rq) && !survivors[rq.kind] {
				addRemovalEdges(i, rq)
			}
		}
	}
}
