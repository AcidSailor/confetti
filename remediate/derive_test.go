package remediate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

// refOrderSchema declares the referrer before the definition so reference edges must correct add order.
func refOrderSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("member {{ id:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().Ref("id", "grp.id")
	s.Node("grp {{ id:vlan }}").Card(schema.ZeroToN).Kind("grp").Key("id")
	return s
}

// refOrderReplaceSchema uses stable member identity and a separate reference field to exercise reference edges through ckReplace.
func refOrderReplaceSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("member {{ id:vlan }} tag {{ tg:vlan }}").
		Card(schema.ZeroToN).Kind("mem").Key("id").Ref("tg", "grp.id")
	s.Node("grp {{ id:vlan }}").Card(schema.ZeroToN).Kind("grp").Key("id")
	return s
}

func orderedDiff(
	t *testing.T, s *schema.Schema, running, intended string,
) (string, *diag.Diagnostics) {
	t.Helper()
	res, d := Diff(mustParse(t, s, running), mustParse(t, s, intended),
		Options{Cycle: Abort})
	return render.Render(res.Tree), d
}

func TestRefEdgeOrdersDefinitionBeforeReferrerOnAdd(t *testing.T) {
	out, d := orderedDiff(t, refOrderSchema(),
		"",
		"interface Ethernet1\n  member 10\ngrp 10\n")
	require.False(t, d.HasErrors(), d.String())
	// declaration rank says interface first; the ref edge must invert that
	assert.Equal(t, "grp 10\ninterface Ethernet1\n  member 10\n", out)
}

func TestRefEdgeOrdersReferrerBeforeDefinitionOnRemove(t *testing.T) {
	out, d := orderedDiff(t, refOrderSchema(),
		"interface Ethernet1\n  member 10\ngrp 10\n",
		"")
	require.False(t, d.HasErrors(), d.String())
	// removes descend by rank => "no grp 10" would come first; edge inverts
	assert.Equal(t, "no interface Ethernet1\nno grp 10\n", out)
}

func TestRefEdgeMatchesCompositeKeyTarget(t *testing.T) {
	// The definition's key is composite (id, z); the ref names only grp.id.
	// CommitCheck resolves this shape through its per-key-argument index; ordering
	// must derive the edge from the same identity model.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("member {{ id:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().Ref("id", "grp.id")
	s.Node("grp {{ id:vlan }} zone {{ z:word }}").
		Card(schema.ZeroToN).Kind("grp").Key("id", "z")
	out, d := orderedDiff(t, s,
		"",
		"interface Ethernet1\n  member 10\ngrp 10 zone a\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"grp 10 zone a\ninterface Ethernet1\n  member 10\n", out)
}

func TestRetargetModifyEdges(t *testing.T) {
	s := refOrderSchema()
	running := mustParse(t, s, "grp 10\ninterface Ethernet1\n  member 10\n")
	intended := mustParse(t, s, "grp 20\ninterface Ethernet1\n  member 20\n")
	d := diag.New()
	dv := &differ{
		running: running, intended: intended,
		order: buildOrderIndex(s), cycle: Abort, d: d,
	}
	dv.collect(running.Root, intended.Root, nil, nil)
	dv.buildGraph()
	g := dv.g
	require.False(t, d.HasErrors(), d.String())

	idx := func(text string) int {
		for _, v := range g.Ops() {
			if v.Text == text {
				return v.Index
			}
		}
		t.Fatalf("no op %q", text)
		return -1
	}
	// modify waits for the new target and precedes the old target's removal
	assert.True(t, g.HasEdge(idx("grp 20"), idx("member 20")))
	assert.True(t, g.HasEdge(idx("member 20"), idx("no grp 10")))
}

func TestRetargetReplaceEdges(t *testing.T) {
	s := refOrderReplaceSchema()
	running := mustParse(
		t,
		s,
		"grp 10\ninterface Ethernet1\n  member 5 tag 10\n",
	)
	intended := mustParse(
		t,
		s,
		"grp 20\ninterface Ethernet1\n  member 5 tag 20\n",
	)
	d := diag.New()
	dv := &differ{
		running: running, intended: intended,
		order: buildOrderIndex(s), cycle: Abort, d: d,
	}
	dv.collect(running.Root, intended.Root, nil, nil)
	dv.buildGraph()
	g := dv.g
	require.False(t, d.HasErrors(), d.String())

	idx := func(text string) int {
		for _, v := range g.Ops() {
			if v.Text == text {
				return v.Index
			}
		}
		t.Fatalf("no op %q", text)
		return -1
	}
	memberIdx := idx("member 5 tag 20")
	// the paired leaf must actually classify as Replace, not degrade to Modify
	var action graph.Action
	for _, v := range g.Ops() {
		if v.Index == memberIdx {
			action = v.Action
		}
	}
	assert.Equal(t, graph.Replace, action)

	// replace waits for the new target and precedes the old target's removal
	assert.True(t, g.HasEdge(idx("grp 20"), memberIdx))
	assert.True(t, g.HasEdge(memberIdx, idx("no grp 10")))
}

func TestRefsOfMalformedListWarnsNotSilent(t *testing.T) {
	// Warn when a malformed reference list prevents creation of ordering edges.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("switchport trunk allowed vlan {{ vlans:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		List("vlans", "uint").Ref("vlans", "vlan.id")
	running := mustParse(
		t,
		s,
		"vlan 10\ninterface Ethernet1\n  switchport trunk allowed vlan 10,,20\n",
	)
	intended := mustParse(t, s, "")
	_, d := Diff(running, intended, Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "ref-ordering edges")
	assert.Contains(t, d.String(), `"10,,20"`)
}

func TestRequiresRemovalPrecedesEveryDefinerRemoval(t *testing.T) {
	// The requiring node must precede both prerequisite removals through the indexed Remove path.
	out, d := orderedDiff(t, requireSchema(),
		"router bgp\nfeature a\nfeature b\n",
		"")
	require.False(t, d.HasErrors(), d.String())
	router := strings.Index(out, "no router bgp")
	require.GreaterOrEqual(t, router, 0, out)
	assert.Less(t, router, strings.Index(out, "no feature a"), out)
	assert.Less(t, router, strings.Index(out, "no feature b"), out)
}

func TestCrossDefModifyOldRequiresOrdersBeforeTargetRemoval(t *testing.T) {
	s := schema.New()
	targets := s.Node("targets").Card(schema.ZeroToOne)
	targets.Child("gate").Card(schema.ZeroToOne).Kind("gate")
	features := s.Node("features").Card(schema.ZeroToOne)
	features.Child("feature on").Card(schema.ZeroToOne).
		Kind("feature").Requires("gate")
	features.Child("feature off").Card(schema.ZeroToOne).
		Kind("feature").MarkIdempotent()
	out, d := orderedDiff(t, s,
		"targets\n  gate\nfeatures\n  feature on\n",
		"targets\nfeatures\n  feature off\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "features\n  feature off\ntargets\n  no gate\n", out)
}

// moveSchema: a keyed child that can migrate between sections.
func moveSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("hold {{ v:word }}").Card(schema.ZeroToN).Kind("hold").Key("v")
	return s
}

func TestMoveEdgeFreesBeforeReclaim(t *testing.T) {
	// A move edge must release the resource in the later section before the earlier section claims it.
	out, d := orderedDiff(t, moveSchema(),
		"interface Ethernet1\ninterface Ethernet2\n  hold x\n",
		"interface Ethernet1\n  hold x\ninterface Ethernet2\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface Ethernet2\n  no hold x\ninterface Ethernet1\n  hold x\n",
		out)
}

func TestOppositeMovesSplitASection(t *testing.T) {
	out, d := orderedDiff(t, moveSchema(),
		"interface Ethernet1\n  hold x\ninterface Ethernet2\n  hold y\n",
		"interface Ethernet1\n  hold y\ninterface Ethernet2\n  hold x\n")
	require.False(t, d.HasErrors(), d.String())
	// No single-visit order satisfies both moves: Ethernet1 is re-entered.
	assert.Equal(t,
		"interface Ethernet1\n  no hold x\n"+
			"interface Ethernet2\n  hold x\n  no hold y\n"+
			"interface Ethernet1\n  hold y\n",
		out)
}

func TestUniqueScopeDerivesMoveOnPartialKeyMatch(t *testing.T) {
	s := schema.New()
	s.Node("slot {{ id:word }} owner {{ own:word }}").
		Card(schema.ZeroToN).Kind("slot").Key("id", "own").Unique("id")
	out, d := orderedDiff(t, s,
		"slot 1 owner alpha\n",
		"slot 1 owner beta\n")
	require.False(t, d.HasErrors(), d.String())
	// full keys differ (owner changed) so pairing makes add+remove; Unique(id)
	// says id itself is exclusive => free it before re-claiming
	assert.Equal(t, "no slot 1 owner alpha\nslot 1 owner beta\n", out)
}

// prioListSchema returns a keyed schema with a unique list argument.
func prioListSchema() *schema.Schema {
	s := schema.New()
	s.Node("priority {{ ids:word }} value {{ value:word }}").
		Card(schema.ZeroToN).Kind("prio").Key("value").Unique("ids").
		List("ids", "uint").
		ListDelta("priority {{ ids }} value {{ value }}",
			"no priority {{ ids }} value {{ value }}")
	return s
}

func TestUniqueListArgUsesCompactSemanticResources(t *testing.T) {
	s := prioListSchema()
	// Equivalent list spellings must order the release before the claim.
	out, d := orderedDiff(t, s,
		"priority 1-100 value 10\n",
		"priority 1-50,51-100 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"no priority 1-100 value 10\npriority 1-50,51-100 value 20\n", out)

	// Overlap requires ordering without expanding the ranges.
	out, d = orderedDiff(t, s,
		"priority 1-65536 value 10\n",
		"priority 65536-70000 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"no priority 1-65536 value 10\npriority 65536-70000 value 20\n", out)
}

func TestUniqueListArgDisjointMembersDeriveNoMoveEdge(t *testing.T) {
	// Disjoint member sets retain declaration order.
	out, d := orderedDiff(t, prioListSchema(),
		"priority 1-50 value 10\n",
		"priority 51-100 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"priority 51-100 value 20\nno priority 1-50 value 10\n", out)
}

func TestUniqueListArgZeroPaddedMembersConflict(t *testing.T) {
	// Numeric member identity ignores zero padding.
	s := prioListSchema()
	out, d := orderedDiff(t, s,
		"priority 007 value 10\n",
		"priority 7 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no priority 007 value 10\npriority 7 value 20\n", out)

	out, d = orderedDiff(t, s,
		"priority 1-10 value 10\n",
		"priority 007 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no priority 1-10 value 10\npriority 007 value 20\n", out)
}

func TestUniqueListArgMultipleReleasesPrecedeOneClaim(t *testing.T) {
	// Two lines each hold part of the claimed set; both releases must precede it.
	out, d := orderedDiff(t, prioListSchema(),
		"priority 1-50 value 10\npriority 51-100 value 20\n",
		"priority 1-100 value 30\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"no priority 51-100 value 20\nno priority 1-50 value 10\n"+
			"priority 1-100 value 30\n", out)
}

func TestUniqueListArgMoveEdgesDeterministic(t *testing.T) {
	// TestOppositeMovesDeterministic covers scalar resources; this test covers lists.
	var first string
	for range 5 {
		out, d := orderedDiff(t, prioListSchema(),
			"priority 1-50 value 10\npriority 40-90 value 20\n",
			"priority 30-60 value 30\npriority 80-99 value 40\n")
		require.False(t, d.HasErrors(), d.String())
		if first == "" {
			first = out
		}
		assert.Equal(t, first, out)
	}
}

// prioKeywordSchema uses a rest capture for multi-word Except values.
func prioKeywordSchema() *schema.Schema {
	s := schema.New()
	s.Node("priority {{ value:word }} ids {{ ids:rest }}").
		Card(schema.ZeroToN).Kind("prio").Key("value").Unique("ids").
		List("ids", "uint").
		ListKeywords("none", "all", "except", "1-10").
		ListDelta("priority {{ value }} ids {{ ids }}",
			"no priority {{ value }} ids {{ ids }}")
	return s
}

func TestUniqueListArgKeywordSetsCompareByMembers(t *testing.T) {
	// Resolve compares keyword spellings by their semantic member sets.
	s := prioKeywordSchema()

	// All overlaps every non-empty subset of its domain.
	out, d := orderedDiff(t, s,
		"priority 10 ids all\n",
		"priority 20 ids 3-5\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"no priority 10 ids all\npriority 20 ids 3-5\n", out)

	// Except does not overlap an excluded member.
	out, d = orderedDiff(t, s,
		"priority 10 ids except 5\n",
		"priority 20 ids 5\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"priority 20 ids 5\nno priority 10 ids except 5\n", out)

	// None does not overlap any set, including itself.
	out, d = orderedDiff(t, s,
		"priority 10 ids none\n",
		"priority 20 ids none\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"priority 20 ids none\nno priority 10 ids none\n", out)
}

func TestUniqueListArgSeparatorMismatchConflictsBothDirections(t *testing.T) {
	// Each definition must resolve its list with its own separator in both directions.
	build := func() *schema.Schema {
		s := prioListSchema()
		s.Node("backup {{ ids:word }} value {{ value:word }}").
			Card(schema.ZeroToN).Kind("prio").Key("value").Unique("ids").
			List("ids", "uint").ListSep(";").
			ListDelta("backup {{ ids }} value {{ value }}",
				"no backup {{ ids }} value {{ value }}")
		return s
	}
	out, d := orderedDiff(t, build(),
		"backup 1;5 value 10\n", "priority 5 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no backup 1;5 value 10\npriority 5 value 20\n", out)

	out, d = orderedDiff(t, build(),
		"priority 5 value 10\n", "backup 1;5 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no priority 5 value 10\nbackup 1;5 value 20\n", out)

	out, d = orderedDiff(t, build(),
		"priority 9 value 10\n", "backup 1;5 value 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "backup 1;5 value 20\nno priority 9 value 10\n", out)
}

func TestUniqueListArgMalformedListWarnsNotSilent(t *testing.T) {
	out, d := orderedDiff(t, prioListSchema(), "priority 100-1 value 10\n", "")
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "unresolvable list")
	assert.Contains(t, d.String(), `"100-1"`)
	assert.Equal(t, "no priority 100-1 value 10\n", out)
}

func TestHeldResourceStringNamesListMembers(t *testing.T) {
	// Cycle warnings must retain list members that the bucket key excludes.
	cfg := mustParse(t, prioListSchema(), "priority 1-50,51-100 value 10\n")
	d := diag.New()
	held := resourcesHeld(topNode(cfg, 0), d)
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, held, 1)
	assert.Equal(t, `prio "1-100"`, held[0].String())
	// The bucket key excludes the list.
	assert.Equal(t, "", held[0].key)
}

func TestOppositeMovesDeterministic(t *testing.T) {
	var first string
	for range 5 {
		out, d := orderedDiff(t, moveSchema(),
			"interface Ethernet1\n  hold x\ninterface Ethernet2\n  hold y\n",
			"interface Ethernet1\n  hold y\ninterface Ethernet2\n  hold x\n")
		require.False(t, d.HasErrors())
		if first == "" {
			first = out
		}
		assert.Equal(t, first, out)
	}
}

// requireSchema declares the REQUIRER before the prerequisite kind, so rank
// alone orders both directions wrong.
func requireSchema() *schema.Schema {
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("feature")
	s.Node("feature {{ name:word }}").
		Card(schema.ZeroToN).Kind("feature").Key("name")
	return s
}

func TestRequiresOrdersPrerequisiteFirstOnAdd(t *testing.T) {
	out, d := orderedDiff(t, requireSchema(),
		"",
		"router bgp\nfeature bgp\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "feature bgp\nrouter bgp\n", out)
}

func TestRequiresOrdersDependentRemovalFirst(t *testing.T) {
	out, d := orderedDiff(t, requireSchema(),
		"router bgp\nfeature bgp\n",
		"")
	require.False(t, d.HasErrors(), d.String())
	// removes descend by rank => "no feature bgp" would come first; the
	// requirer's removal must precede the last definer's removal
	assert.Equal(t, "no router bgp\nno feature bgp\n", out)
}

func TestRequiresSurvivorNeedsNoEdge(t *testing.T) {
	// Existing feature BGP in both inputs needs no ordering constraint.
	out, d := orderedDiff(t, requireSchema(),
		"feature bgp\n",
		"feature bgp\nrouter bgp\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "router bgp\n", out)
}

// keylessRequireSchema targets a keyless Kind such as
// a literal "feature bgp" has no capture to key on, but is a legitimate
// existential prerequisite (regression: definesOf/survivingKinds used to
// skip keyless kinds, so the requirer saw "goal defines none").
func keylessRequireSchema() *schema.Schema {
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("gate")
	s.Node("gate on").Card(schema.ZeroToOne).Kind("gate")
	return s
}

func TestRequiresKeylessKindOrdersOnAdd(t *testing.T) {
	out, d := orderedDiff(t, keylessRequireSchema(),
		"",
		"router bgp\ngate on\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "gate on\nrouter bgp\n", out)
}

func TestRequiresKeylessKindSurvives(t *testing.T) {
	out, d := orderedDiff(t, keylessRequireSchema(),
		"gate on\n",
		"gate on\nrouter bgp\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "router bgp\n", out)
}

func TestRequiresTagLabelOrdersOnAdd(t *testing.T) {
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("gate")
	s.Node("gate on").Card(schema.ZeroToOne).Tag("gate")
	out, d := orderedDiff(t, s,
		"",
		"router bgp\ngate on\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "gate on\nrouter bgp\n", out)
}

func TestCrossDefModifyIntroducesRequiredTag(t *testing.T) {
	s := schema.New()
	s.Node("router").Requires("gate")
	s.Node("feature off").Kind("feature").MarkIdempotent()
	s.Node("feature on").Kind("feature").MarkIdempotent().Tag("gate")
	out, d := orderedDiff(t, s, "feature off\n", "router\nfeature on\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "feature on\nrouter\n", out)
}

func TestCrossDefModifyIntroducesReferencedTag(t *testing.T) {
	s := schema.New()
	s.Node("member {{ id:uint }}").Card(schema.ZeroToN).Key("id").
		Ref("id", "gate.id")
	s.Node("feature off {{ id:uint }}").Card(schema.ZeroToN).
		Kind("feature").Key("id").MarkIdempotent()
	s.Node("feature on {{ id:uint }}").Card(schema.ZeroToN).
		Kind("feature").Key("id").MarkIdempotent().Tag("gate")
	out, d := orderedDiff(
		t,
		s,
		"feature off 10\n",
		"member 10\nfeature on 10\n",
	)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "feature on 10\nmember 10\n", out)
}

func TestRequiresPartialKeyOverlapIsNotSurvival(t *testing.T) {
	// A composite-key definer is replaced (10,a)->(20,a): no whole instance
	// survives, so the Requires edge must still fire despite the shared z=a.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("grp")
	s.Node("grp {{ id:vlan }} zone {{ z:word }}").
		Card(schema.ZeroToN).Kind("grp").Key("id", "z")
	out, d := orderedDiff(t, s,
		"grp 10 zone a\n",
		"grp 20 zone a\nrouter bgp\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "grp 20 zone a\nrouter bgp\nno grp 10 zone a\n", out)
}

func TestRequiresUnknownLabelIsError(t *testing.T) {
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("nosuch")
	_, d := orderedDiff(t, s, "", "router bgp\n")
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `unknown label "nosuch"`)
}

func TestRequiresUnsatisfiedIsErrorUnderBothCycles(t *testing.T) {
	// A goal that neither keeps nor adds a required prerequisite cannot converge.
	for _, cycle := range []Cycle{Abort, Break} {
		s := requireSchema()
		_, d := Diff(
			mustParse(t, s, ""), mustParse(t, s, "router bgp\n"),
			Options{Cycle: cycle})
		require.True(t, d.HasErrors(), "cycle=%v", cycle)
		assert.Contains(t, d.String(), `requires a "feature"`)
	}
}

func TestOrderHookReordersOutput(t *testing.T) {
	s := schema.New()
	s.Node("alpha {{ v:word }}").Card(schema.ZeroToN)
	s.Node("beta {{ v:word }}").Card(schema.ZeroToN)
	s.OrderHook(func(g *graph.Graph) {
		a, b := -1, -1
		for _, v := range g.Ops() {
			switch v.Text {
			case "alpha one":
				a = v.Index
			case "beta two":
				b = v.Index
			}
		}
		if a >= 0 && b >= 0 {
			g.AddEdge(b, a) // quirk: beta must precede alpha
		}
	})
	out, d := orderedDiff(t, s, "", "alpha one\nbeta two\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "beta two\nalpha one\n", out)
}

func TestOrderHookCycleFollowsPolicy(t *testing.T) {
	mk := func() *schema.Schema {
		s := schema.New()
		s.Node("alpha {{ v:word }}").Card(schema.ZeroToN)
		s.Node("beta {{ v:word }}").Card(schema.ZeroToN)
		s.OrderHook(func(g *graph.Graph) {
			g.AddEdge(0, 1)
			g.AddEdge(1, 0)
		})
		return s
	}
	s := mk()
	res, d := Diff(
		mustParse(t, s, ""),
		mustParse(t, s, "alpha one\nbeta two\n"),
		Options{Cycle: Abort},
	)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "ordering cycle")
	assert.True(t, res.Empty()) // strict: no artifact

	s2 := mk()
	res2, d2 := Diff(
		mustParse(t, s2, ""),
		mustParse(t, s2, "alpha one\nbeta two\n"),
		Options{Cycle: Break},
	)
	assert.False(t, d2.HasErrors())
	assert.Contains(t, d2.String(), "dropped ordering edge")
	assert.False(t, res2.Empty()) // lenient: artifact emitted
}

func TestCycleBreakNamesRefEndToEnd(t *testing.T) {
	// A lenient break of a mutual-reference removal cycle must identify the affected reference.
	s := schema.New()
	s.Node("a {{ x:word }}").Card(schema.ZeroToN).Kind("a").Key("x").
		Ref("x", "b.y")
	s.Node("b {{ y:word }}").Card(schema.ZeroToN).Kind("b").Key("y").
		Ref("y", "a.x")
	running := mustParse(t, s, "a 1\nb 1\n")
	intended := mustParse(t, s, "")
	_, d := Diff(running, intended, Options{Cycle: Break})
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "dropped ordering edge")
	assert.Contains(t, d.String(), "(protecting ref ")
}

func l2l3RemSchema() *schema.Schema {
	s := schema.New()
	iface := s.Node("interface {{ name:word }}").
		Card(schema.ZeroToN).
		Key("name")
	iface.Child("switchport").Card(schema.ZeroToOne).Tag("l2").ExcludeTag("l3")
	iface.Child("switchport access vlan {{ vlan:uint }}").
		Card(schema.ZeroToOne).Tag("l2").ExcludeTag("l3")
	iface.Child("ip address {{ addr:word }}").
		Card(schema.ZeroToOne).Tag("l3").ExcludeTag("l2")
	return s
}

func TestExcludeTagOrdersRemovalBeforeAdd(t *testing.T) {
	// The device rejects an address until all switched-port lines are removed.
	out, d := orderedDiff(t, l2l3RemSchema(),
		"interface Ethernet1\n  switchport\n  switchport access vlan 20\n",
		"interface Ethernet1\n  ip address 10.0.0.1/24\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "interface Ethernet1\n  no switchport access vlan 20\n"+
		"  no switchport\n  ip address 10.0.0.1/24\n", out)
}

func TestExcludeTagOrdersRemovalBeforeAddReverse(t *testing.T) {
	out, d := orderedDiff(t, l2l3RemSchema(),
		"interface Ethernet1\n  ip address 10.0.0.1/24\n",
		"interface Ethernet1\n  switchport\n  switchport access vlan 20\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "interface Ethernet1\n  no ip address 10.0.0.1/24\n"+
		"  switchport\n  switchport access vlan 20\n", out)
}

func TestExcludeTagOrdersSemanticRemovalBeforeAdd(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*schema.Def)
		running   string
		intended  string
		first     string
	}{
		{
			name: "replace",
			configure: func(iface *schema.Def) {
				iface.Child("mode l2").Kind("mode").Tag("l2").ExcludeTag("l3")
				iface.Child("mode l3").Kind("mode").Tag("l3").ExcludeTag("l2")
			},
			running:  "mode l2\n",
			intended: "extra l3\nmode l3\n",
			first:    "no mode l2",
		},
		{
			name: "modify",
			configure: func(iface *schema.Def) {
				iface.Child("mode l2").
					Kind("mode").
					MarkIdempotent().
					Tag("l2").
					ExcludeTag("l3")
				iface.Child("mode l3").
					Kind("mode").
					MarkIdempotent().
					Tag("l3").
					ExcludeTag("l2")
			},
			running:  "mode l2\n",
			intended: "extra l3\nmode l3\n",
			first:    "mode l3\n",
		},
		{
			name: "toggle",
			configure: func(iface *schema.Def) {
				l2 := iface.Child("mode l2").Tag("l2").ExcludeTag("l3")
				iface.Child("mode l3").Tag("l3").ExcludeTag("l2").Toggles(l2)
			},
			running:  "mode l2\n",
			intended: "extra l3\nmode l3\n",
			first:    "mode l3\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := schema.New()
			iface := s.Node("interface")
			iface.Child("extra l3").Tag("l3").ExcludeTag("l2")
			tt.configure(iface)
			out, d := orderedDiff(t, s,
				"interface\n  "+tt.running,
				"interface\n  "+strings.ReplaceAll(tt.intended, "\n", "\n  "))
			require.False(t, d.HasErrors(), d.String())
			first := strings.Index(out, tt.first)
			extra := strings.Index(out, "extra l3")
			require.GreaterOrEqual(t, first, 0, out)
			require.GreaterOrEqual(t, extra, 0, out)
			assert.Less(t, first, extra, out)
		})
	}
}

func TestExcludeTagIgnoresOtherParentInstances(t *testing.T) {
	out, d := orderedDiff(t, l2l3RemSchema(),
		"interface Ethernet1\n  switchport\n",
		"interface Ethernet1\n  switchport\ninterface Ethernet2\n"+
			"  ip address 10.0.0.1/24\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface Ethernet2\n  ip address 10.0.0.1/24\n", out)
}

func TestRefEdgeThroughTagLabelOrdersOnAdd(t *testing.T) {
	s := schema.New()
	s.Node("grp {{ id:uint }}").Card(schema.ZeroToN).Key("id").Tag("bridge")
	s.Node("member {{ v:uint }}").Card(schema.ZeroToN).Key("v").
		Ref("v", "bridge.id")
	out, d := orderedDiff(t, s, "", "member 10\ngrp 10\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "grp 10\nmember 10\n", out)
}

func TestRequiresSharedLabelAcrossDefinitionsIsNotSurvival(t *testing.T) {
	// Different definitions with one label do not provide continuous presence.
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("gate")
	s.Node("feature lacp").Card(schema.ZeroToOne).Tag("gate")
	s.Node("feature vpc").Card(schema.ZeroToOne).Tag("gate")
	out, d := orderedDiff(t, s, "feature lacp\n", "router bgp\nfeature vpc\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "feature vpc\nrouter bgp\nno feature lacp\n", out)
}

func TestRequiresSatisfiedByBaseline(t *testing.T) {
	// A device-provided prerequisite is neither kept nor added, yet the goal converges.
	for _, cycle := range []Cycle{Abort, Break} {
		s := requireSchema()
		res, d := Diff(
			mustParse(t, s, ""), mustParse(t, s, "router bgp\n"),
			Options{Cycle: cycle, Baseline: mustParse(t, s, "feature bgp\n")})
		require.False(t, d.HasErrors(), d.String())
		assert.Equal(t, "router bgp\n", render.Render(res.Tree))
	}
}

func TestBaselineNeverEntersThePlan(t *testing.T) {
	// Removing the last user of a baseline prerequisite must not negate the baseline.
	s := requireSchema()
	res, d := Diff(
		mustParse(t, s, "router bgp\n"), mustParse(t, s, ""),
		Options{Baseline: mustParse(t, s, "feature bgp\n")})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no router bgp\n", render.Render(res.Tree))
}

func TestBaselineSchemaMismatchIsError(t *testing.T) {
	s := requireSchema()
	res, d := Diff(
		mustParse(t, s, ""), mustParse(t, s, "router bgp\n"),
		Options{Baseline: mustParse(t, requireSchema(), "feature bgp\n")})
	require.True(t, d.HasErrors())
	assert.Contains(
		t,
		d.String(),
		"baseline and intended use different schemas",
	)
	assert.True(t, res.Empty())
}

func TestBaselineRemovalIsError(t *testing.T) {
	// Running prints a device-provided object; a goal that omits it cannot negate it.
	s := requireSchema()
	res, d := Diff(
		mustParse(t, s, "feature bgp\nrouter bgp\n"),
		mustParse(t, s, "router bgp\n"),
		Options{Baseline: mustParse(t, s, "feature bgp\n")})
	require.True(t, d.HasErrors())
	assert.Contains(
		t,
		d.String(),
		`no feature bgp: removes device-provided feature "bgp" declared by the baseline`,
	)
	// The result stays available for inspection.
	assert.Equal(t, "no feature bgp\n", render.Render(res.Tree))
}

// baselineIdentSchema gives one composite-keyed def and two keyless defs that share a Kind.
func baselineIdentSchema() *schema.Schema {
	s := schema.New()
	s.Node("route-map {{ name:word }} permit {{ seq:word }}").
		Card(schema.ZeroToN).Kind("rm").Key("name", "seq")
	s.Node("feature bgp").Card(schema.ZeroToOne).Kind("feat")
	s.Node("feature ospf").Card(schema.ZeroToOne).Kind("feat")
	return s
}

// One shared component of a composite key is not the same object.
func TestBaselineCompositeKeyMatchesInFull(t *testing.T) {
	s := baselineIdentSchema()
	_, d := Diff(
		mustParse(t, s, "route-map OTHER permit 10\n"),
		mustParse(t, s, ""),
		Options{Baseline: mustParse(t, s, "route-map RESERVED permit 10\n")})
	assert.False(t, d.HasErrors(), d.String())

	// The same key in full still reports.
	_, same := Diff(
		mustParse(t, s, "route-map RESERVED permit 10\n"),
		mustParse(t, s, ""),
		Options{Baseline: mustParse(t, s, "route-map RESERVED permit 10\n")})
	assert.True(t, same.HasErrors(), same.String())
}

// Definitions sharing a label are distinct objects.
func TestBaselineKeylessLabelDoesNotCollideAcrossDefinitions(t *testing.T) {
	s := baselineIdentSchema()
	_, d := Diff(
		mustParse(t, s, "feature ospf\n"),
		mustParse(t, s, ""),
		Options{Baseline: mustParse(t, s, "feature bgp\n")})
	assert.False(t, d.HasErrors(), d.String())

	_, same := Diff(
		mustParse(t, s, "feature bgp\n"),
		mustParse(t, s, ""),
		Options{Baseline: mustParse(t, s, "feature bgp\n")})
	assert.True(t, same.HasErrors(), same.String())
}

// An idempotent reissue changes a value without emitting a negation.
func TestBaselineValueChangeIsNotANegation(t *testing.T) {
	s := schema.New()
	s.Node("vlan {{ id:word }} name {{ nm:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id").MarkIdempotent()
	res, d := Diff(
		mustParse(t, s, "vlan 1 name old\n"),
		mustParse(t, s, "vlan 1 name new\n"),
		Options{Baseline: mustParse(t, s, "vlan 1 name default\n")})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan 1 name new\n", render.Render(res.Tree))
}

// A toggle flip supersedes its partner without emitting a negation.
func TestBaselineToggleFlipIsNotANegation(t *testing.T) {
	s := schema.New()
	up := s.Node("no shutdown").Card(schema.ZeroToOne)
	down := s.Node("shutdown").Card(schema.ZeroToOne).Kind("admin-down")
	up.Toggles(down)
	res, d := Diff(
		mustParse(t, s, "shutdown\n"),
		mustParse(t, s, "no shutdown\n"),
		Options{Baseline: mustParse(t, s, "shutdown\n")})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no shutdown\n", render.Render(res.Tree))
}

// A replacement does emit a negation, and the diagnostic must name that line.
func TestBaselineReplacementNamesTheNegatedLine(t *testing.T) {
	s := schema.New()
	s.Node("vlan {{ id:word }} name {{ nm:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	_, d := Diff(
		mustParse(t, s, "vlan 1 name old\n"),
		mustParse(t, s, "vlan 1 name new\n"),
		Options{Baseline: mustParse(t, s, "vlan 1 name default\n")})
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(),
		`no vlan 1 name old: removes device-provided vlan "1"`)
}

// Every device-provided object a single removal negates must be reported.
func TestBaselineRemovalReportsEveryObject(t *testing.T) {
	s := schema.New()
	vrf := s.Node("vrf context {{ name:word }}").
		Card(schema.ZeroToN).Kind("vrf").Key("name")
	vrf.Child("address-family {{ afi:word }}").
		Card(schema.ZeroToN).Kind("af").Key("afi")
	base := "vrf context default\n  address-family ipv4\n" +
		"  address-family ipv6\n"
	_, d := Diff(
		mustParse(t, s, base),
		mustParse(t, s, ""),
		Options{Baseline: mustParse(t, s, base)})
	require.True(t, d.HasErrors())
	assert.Equal(t, 3, len(d.Items), d.String())
}

func TestBaselineKeptInBothIsNotAnError(t *testing.T) {
	s := requireSchema()
	_, d := Diff(
		mustParse(t, s, "feature bgp\n"),
		mustParse(t, s, "feature bgp\nrouter bgp\n"),
		Options{Baseline: mustParse(t, s, "feature bgp\n")})
	assert.False(t, d.HasErrors(), d.String())
}

// aclNamespaceSchema returns two Kinds that share one device-side name space.
func aclNamespaceSchema(namespace bool) *schema.Schema {
	s := schema.New()
	ip := s.Node("ip access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("ip-access-list").Tag("access-list").Key("name")
	mac := s.Node("mac access-list {{ name:word }}").Card(schema.ZeroToN).
		Kind("mac-access-list").Tag("access-list").Key("name")
	if namespace {
		ip.Namespace("access-list")
		mac.Namespace("access-list")
	}
	return s
}

func TestNamespaceFreesNameAcrossKindsBeforeClaim(t *testing.T) {
	out, d := orderedDiff(t, aclNamespaceSchema(true),
		"ip access-list L\n",
		"mac access-list L\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no ip access-list L\nmac access-list L\n", out)
}

func TestNamespaceDistinctNamesKeepDeclarationOrder(t *testing.T) {
	out, d := orderedDiff(t, aclNamespaceSchema(true),
		"ip access-list L\n",
		"mac access-list M\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "mac access-list M\nno ip access-list L\n", out)
}

// boxClaimSchema holds a claim under a box, optionally scoping the name space to one box.
func boxClaimSchema(scoped bool) *schema.Schema {
	s := schema.New()
	box := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	claim := box.Child("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id")
	if scoped {
		claim.ScopedBy(box)
	}
	return s
}

// An undeclared extent keeps the conservative device-wide assumption in ordering.
func TestUnscopedClaimFreesAcrossOwnersBeforeClaim(t *testing.T) {
	out, d := orderedDiff(t, boxClaimSchema(false),
		"box a\n  claim 1\n",
		"box b\n  claim 1\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Less(t, strings.Index(out, "no box a"), strings.Index(out, "box b"))
}

// ScopedBy says the two boxes hold different names, so no release must precede the claim.
func TestScopedByDerivesNoMoveEdgeAcrossAnchors(t *testing.T) {
	out, d := orderedDiff(t, boxClaimSchema(true),
		"box a\n  claim 1\n",
		"box b\n  claim 1\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Less(t, strings.Index(out, "box b"), strings.Index(out, "no box a"))
}

func TestTagWithoutNamespaceDerivesNoMoveEdge(t *testing.T) {
	// Tags classify; only Namespace opts a label into exclusivity.
	out, d := orderedDiff(t, aclNamespaceSchema(false),
		"ip access-list L\n",
		"mac access-list L\n")
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "mac access-list L\nno ip access-list L\n", out)
}
