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
)

// resource identifies a labeled or exclusive value.
type resource struct {
	label string
	arg   string
	def   *schema.Def
	key   string
}

// String renders a resource for diagnostics.
func (r resource) String() string {
	if r.key == "" {
		return r.label
	}
	return fmt.Sprintf("%s %q", r.label,
		strings.ReplaceAll(r.key, "\x00", ","))
}

// heldResource adds list members and cycle-warning text to a resource.
type heldResource struct {
	resource
	members listval.Members
	display string
	isList  bool
}

// buildGraph derives ordering edges among planned operations and then runs schema OrderHooks in registration order.
func (dv *differ) buildGraph() {
	dv.g = graph.New(opViews(dv.ops))
	dv.why = map[[2]int]string{}
	dv.deriveSlotCleanupEdges()
	dv.deriveRefEdges()
	dv.deriveMoveEdges()
	dv.deriveRequireEdges()
	dv.deriveExclusionEdges()
	for _, h := range dv.intended.Schema.OrderHooks {
		h(dv.g)
	}
}

// definesOf returns one resource per label and key argument in a subtree so references can target one part of a composite key.
func definesOf(n *schema.Node) []resource {
	var out []resource
	n.Walk(func(x *schema.Node) { out = appendDefines(out, x) })
	return out
}

// appendDefines appends the resources one node defines.
func appendDefines(out []resource, x *schema.Node) []resource {
	def := x.Def
	if def == nil {
		return out
	}
	for _, label := range def.Labels() {
		// Keyless labels satisfy presence-only Requires relations.
		if len(def.KeyArgs) == 0 {
			out = append(out, resource{label: label})
			continue
		}
		for _, a := range def.KeyArgs {
			out = append(out, resource{label: label, arg: a, key: x.Fields[a]})
		}
	}
	return out
}

// refsOf returns referenced resources in a subtree and expands semantic list values into separate references.
func refsOf(n *schema.Node, d *diag.Diagnostics) []resource {
	var out []resource
	n.Walk(func(x *schema.Node) { out = appendRefs(out, x, d) })
	return out
}

// appendRefs appends the resources one node references, expanding a semantic list into separate references.
func appendRefs(
	out []resource,
	x *schema.Node,
	d *diag.Diagnostics,
) []resource {
	def := x.Def
	if def == nil {
		return out
	}
	for _, r := range def.Relations {
		if !r.IsRef() {
			continue
		}
		if ls := def.ListSpec; ls.Arg != "" && ls.Arg == r.FromArg {
			items, ok := resolveListArg(x, ls, d, "ref-ordering edges")
			if !ok {
				continue
			}
			for _, it := range items {
				out = append(out, resource{
					label: r.Label, arg: r.TargetKey, key: it,
				})
			}
			continue
		}
		out = append(out, resource{
			label: r.Label,
			arg:   r.TargetKey,
			key:   x.Fields[r.FromArg],
		})
	}
	return out
}

// addedSubtree returns the subtree created by an operation, including replacement and cross-definition modification results.
func addedSubtree(o op) *schema.Node {
	if o.action == graph.Add || o.action == graph.Replace {
		return o.src
	}
	if o.action == graph.Modify && o.runSrc != nil &&
		o.src.Def != o.runSrc.Def {
		return o.src
	}
	return nil
}

// removedSubtree returns the subtree removed by an operation, including replaced and cross-definition modified inputs.
func removedSubtree(o op) *schema.Node {
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

// negatedSubtree is the emitted-line counterpart of removedSubtree: the subtree an operation negates, or nil when it emits no negation.
func negatedSubtree(o op) *schema.Node {
	switch o.action {
	case graph.Remove:
		return o.src
	case graph.Replace:
		return o.runSrc
	}
	// A toggle flip, a list delta, and an idempotent reissue change a value without negating it.
	return nil
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

// excludes returns the label by which a forbids b among siblings.
func excludes(a, b *schema.Def) (string, bool) {
	for _, r := range a.Relations {
		if r.IsExclusion() && b.HasLabel(r.Label) {
			return r.Label, true
		}
	}
	return "", false
}

// conflictLabel returns a label that makes two sibling definitions mutually exclusive in either direction.
func conflictLabel(a, b *schema.Def) (string, bool) {
	if a == nil || b == nil {
		return "", false
	}
	if label, ok := excludes(a, b); ok {
		return label, true
	}
	return excludes(b, a)
}

// deriveExclusionEdges removes conflicting siblings before it installs their excluder.
func (dv *differ) deriveExclusionEdges() {
	for i, add := range dv.ops {
		added := addedSubtree(add)
		if added == nil {
			continue
		}
		for j, rm := range dv.ops {
			removed := removedSubtree(rm)
			if i == j || removed == nil ||
				!slices.Equal(rm.secs, add.secs) {
				continue
			}
			if label, ok := conflictLabel(added.Def, removed.Def); ok {
				dv.addEdge(j, i, "mutually exclusive label %q", label)
			}
		}
	}
}

// definedIdents returns one resource per label in a subtree, keyed by definition and full key value so identity is exact.
func definedIdents(n *schema.Node) []resource {
	var out []resource
	n.Walk(func(x *schema.Node) {
		def := x.Def
		if def == nil {
			return
		}
		// Definitions sharing a label are distinct objects, so keep the def.
		for _, label := range def.Labels() {
			out = append(out, resource{
				label: label, def: def, key: ident.KeyValue(x),
			})
		}
	})
	return out
}

// negatedPath renders the emitted negation line with its kept sections.
func negatedPath(o op) string {
	if o.pre != nil {
		return opPathAt(o.secs, o.pre)
	}
	return opPathAt(o.secs, o.node)
}

// checkBaselineRemovals reports each operation whose plan negates an object the baseline declares as device-provided.
func (dv *differ) checkBaselineRemovals() {
	if len(dv.provided) == 0 {
		return
	}
	for _, o := range dv.ops {
		rm := negatedSubtree(o)
		if rm == nil {
			continue
		}
		// Report every distinct baseline object the operation negates, once each.
		var seen map[resource]bool
		for _, r := range definedIdents(rm) {
			if !dv.provided[r] || seen[r] {
				continue
			}
			if seen == nil {
				seen = map[resource]bool{}
			}
			seen[r] = true
			dv.d.Add(
				diag.Error,
				"%s: removes device-provided %s declared by the baseline",
				negatedPath(o), r,
			)
		}
	}
}

// definerIndex maps each resource defined by the selected subtrees to the operation with the smallest baseline key.
func definerIndex(ops []op, pick func(op) *schema.Node) map[resource]int {
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

// addEdge records an ordering edge and its first cycle-warning reason.
func (dv *differ) addEdge(from, to int, reason string, args ...any) {
	dv.g.AddEdge(from, to)
	if k := [2]int{from, to}; dv.why[k] == "" {
		dv.why[k] = fmt.Sprintf(reason, args...)
	}
}

// deriveRefEdges orders definition adds before referrer adds and referrer removals before definition removals, including retargeting operations.
func (dv *differ) deriveRefEdges() {
	addDef := definerIndex(dv.ops, addedSubtree)
	rmDef := definerIndex(dv.ops, removedSubtree)
	for i, o := range dv.ops {
		if o.action != graph.Remove {
			for _, r := range refsOf(o.src, dv.d) {
				if j, ok := addDef[r]; ok && j != i {
					dv.addEdge(j, i, "ref %s.%s=%q", r.label, r.arg, r.key)
				}
			}
		}
		// A Remove op deletes o.src; every other action can retarget away from runSrc or a superseded toggle partner.
		// Do not visit a Replace running subtree twice.
		rm := removedSubtree(o)
		if rm == o.runSrc {
			rm = nil
		}
		for _, src := range [2]*schema.Node{o.runSrc, rm} {
			if src == nil {
				continue
			}
			for _, r := range refsOf(src, dv.d) {
				if j, ok := rmDef[r]; ok && j != i {
					dv.addEdge(i, j, "ref %s.%s=%q", r.label, r.arg, r.key)
				}
			}
		}
	}
}

// resourcesHeld returns comparable exclusive resources and warns about unresolvable lists.
func resourcesHeld(n *schema.Node, d *diag.Diagnostics) []heldResource {
	var out []heldResource
	n.Walk(func(x *schema.Node) { out = appendHeld(out, x, d) })
	return out
}

// appendHeld appends the exclusive resource one node holds and warns about an unresolvable list.
func appendHeld(
	out []heldResource,
	x *schema.Node,
	d *diag.Diagnostics,
) []heldResource {
	def := x.Def
	if def == nil || len(def.KeyArgs) == 0 {
		return out
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
			return out
		}
		parts[listIdx] = listval.Canonical(items, ls.Sep, ls.Keywords())
		held.members = listval.Intervals(items)
		held.display = strings.Join(parts, "\x00")
		held.isList = true
	}
	return append(out, held)
}

// resolveListArg resolves a list argument and warns when ordering must skip the line.
func resolveListArg(
	x *schema.Node,
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
func resourceFor(def *schema.Def, key string) resource {
	r := resource{label: def.KindName, key: key}
	if r.label == "" {
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
					dv.addEdge(old.op, i, "exclusive resource %s", r)
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

// String formats the exclusive resource and value for cycle warnings.
func (a heldResource) String() string {
	name := a.label
	if name == "" && a.def != nil {
		name = a.def.Template
	}
	return fmt.Sprintf("%s %q", name,
		strings.ReplaceAll(a.display, "\x00", ","))
}

// requirement is one Requires declaration found in an op's subtree.
type requirement struct {
	tmpl  string // The owning definition template used in diagnostics.
	label string
}

// requirementsOf returns the Requires declarations in a subtree.
func requirementsOf(n *schema.Node) []requirement {
	var out []requirement
	n.Walk(func(x *schema.Node) { out = appendRequirements(out, x) })
	return out
}

// appendRequirements appends the Requires declarations one node carries.
func appendRequirements(out []requirement, x *schema.Node) []requirement {
	def := x.Def
	if def == nil {
		return out
	}
	for _, r := range def.Relations {
		if r.IsRequires() {
			out = append(out, requirement{tmpl: def.Template, label: r.Label})
		}
	}
	return out
}

// labelResources returns every label instance in a configuration, keyed by definition and key value.
func labelResources(c *schema.Config) map[resource]bool {
	set := map[resource]bool{}
	for _, r := range definedIdents(c.Root) {
		set[r] = true
	}
	return set
}

// survivingLabels returns labels provided by the same definition and key in both configurations or by any provided resource.
func survivingLabels(
	running, intended *schema.Config,
	provided map[resource]bool,
) map[string]bool {
	run, want := labelResources(running), labelResources(intended)
	out := map[string]bool{}
	for r := range run {
		if want[r] {
			out[r.label] = true
		}
	}
	// Label-only on purpose: a device-provided object is permanent, so every label it carries survives.
	for r := range provided {
		out[r.label] = true
	}
	return out
}

// deriveRequireEdges orders Requires users after prerequisite adds and before prerequisite removals when no instance survives.
func (dv *differ) deriveRequireEdges() {
	ops, d := dv.ops, dv.d
	survivors := survivingLabels(dv.running, dv.intended, dv.provided)

	// The order index already contains every reachable definition.
	declared := map[string]bool{}
	for n := range dv.order {
		for _, label := range n.Labels() {
			declared[label] = true
		}
	}

	// Index the first add for each label by the smallest baseline key.
	addByLabel := map[string]int{}
	for i, o := range ops {
		src := addedSubtree(o)
		if src == nil {
			continue
		}
		for _, r := range definesOf(src) {
			if j, ok := addByLabel[r.label]; !ok ||
				ops[i].key.compare(ops[j].key) < 0 {
				addByLabel[r.label] = i
			}
		}
	}

	// Index each removal once per label.
	removeByLabel := map[string][]int{}
	for j, o := range ops {
		src := removedSubtree(o)
		if src == nil {
			continue
		}
		seen := map[string]bool{}
		for _, r := range definesOf(src) {
			if !seen[r.label] {
				seen[r.label] = true
				removeByLabel[r.label] = append(removeByLabel[r.label], j)
			}
		}
	}

	reported := map[string]bool{}
	validRequirement := func(rq requirement) bool {
		if declared[rq.label] {
			return true
		}
		once := rq.tmpl + "\x00" + rq.label
		if !reported[once] {
			reported[once] = true
			d.Add(
				diag.Error,
				"%s: requires unknown label %q",
				rq.tmpl,
				rq.label,
			)
		}
		return false
	}
	for i, o := range ops {
		// The removal walk covers prerequisites for Remove operations.
		if o.action != graph.Remove {
			for _, rq := range requirementsOf(o.src) {
				if !validRequirement(rq) || survivors[rq.label] {
					continue
				}
				if j, ok := addByLabel[rq.label]; ok {
					if j != i {
						dv.addEdge(j, i, "required label %q", rq.label)
					}
					continue
				}
				// The goal neither keeps nor adds the prerequisite, so the emitted sequence cannot converge.
				d.Add(
					diag.Error,
					"%s: requires a %q but the goal defines none",
					opPath(o), rq.label,
				)
			}
		}
		// Remove prerequisites only after the operations that supersede their users.
		rm := removedSubtree(o)
		if rm == nil {
			continue
		}
		for _, rq := range requirementsOf(rm) {
			if !validRequirement(rq) || survivors[rq.label] {
				continue
			}
			for _, j := range removeByLabel[rq.label] {
				if j != i {
					dv.addEdge(i, j, "required label %q", rq.label)
				}
			}
		}
	}
}
