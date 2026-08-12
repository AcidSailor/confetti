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
		diag.Policy{Strict: true})
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
		order: buildOrderIndex(s), p: diag.Policy{Strict: true}, d: d,
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
		order: buildOrderIndex(s), p: diag.Policy{Strict: true}, d: d,
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
	_, d := Diff(running, intended, diag.Policy{})
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
	// A cross-definition reissue must disable the old user before removing its prerequisite.
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

func TestRequiresUnknownKindIsError(t *testing.T) {
	s := schema.New()
	s.Node("router {{ proto:word }}").Card(schema.ZeroToN).Requires("nosuch")
	_, d := orderedDiff(t, s, "", "router bgp\n")
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `unknown kind "nosuch"`)
}

func TestRequiresUnsatisfiedSeverityFollowsPolicy(t *testing.T) {
	s := requireSchema()
	res, d := Diff(
		mustParse(t, s, ""), mustParse(t, s, "router bgp\n"),
		diag.Policy{Strict: true})
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `requires a "feature"`)
	_ = res

	s2 := requireSchema()
	_, d2 := Diff(
		mustParse(t, s2, ""), mustParse(t, s2, "router bgp\n"),
		diag.Policy{})
	assert.False(t, d2.HasErrors()) // lenient: Warning, artifact still emitted
	assert.Contains(t, d2.String(), `requires a "feature"`)
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
		diag.Policy{Strict: true},
	)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "ordering cycle")
	assert.True(t, res.Empty()) // strict: no artifact

	s2 := mk()
	res2, d2 := Diff(
		mustParse(t, s2, ""),
		mustParse(t, s2, "alpha one\nbeta two\n"),
		diag.Policy{},
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
	_, d := Diff(running, intended, diag.Policy{})
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "dropped ordering edge")
	assert.Contains(t, d.String(), "(protecting ref ")
}
