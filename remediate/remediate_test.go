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
	"github.com/acidsailor/confetti/tree"
)

func remediation(t *testing.T, running, intended string) (string, *Result) {
	t.Helper()
	s := testSchema()
	res, d := Diff(
		mustParse(t, s, running),
		mustParse(t, s, intended),
		diag.Policy{},
	)
	require.False(t, d.HasErrors(), d.String())
	return render.Render(res.Tree), res
}

func TestDiffNoChurn(t *testing.T) {
	out, res := remediation(t,
		"interface Ethernet1/1\n  description A\n",
		"interface Ethernet1/1\n  description A\n")
	assert.True(t, res.Empty())
	assert.Equal(t, "", out)
}

func TestDiffAddLeafAndSection(t *testing.T) {
	out, _ := remediation(t,
		"",
		"interface Ethernet1/1\n  description A\n")
	assert.Equal(t, "interface Ethernet1/1\n  description A\n", out)
}

func TestDiffModifyIdempotent(t *testing.T) { // E2
	out, res := remediation(t,
		"interface Ethernet1/1\n  description A\n",
		"interface Ethernet1/1\n  description B\n")
	assert.Equal(t, "interface Ethernet1/1\n  description B\n", out)
	var sawModify bool
	tree.Walk(res.Tree, func(n *tree.Node) {
		if n.Op == tree.OpModify && n.Text == "description B" {
			sawModify = true
		}
	})
	assert.True(t, sawModify)
}

func TestDiffRemoveLeaf(t *testing.T) { // E3
	out, _ := remediation(t,
		"interface Ethernet1/1\n  description Customer A\n",
		"interface Ethernet1/1\n  switchport access vlan 10\n  no shutdown\n")
	assert.Contains(t, out, "no description Customer A")
}

func TestDiffToggleForward(t *testing.T) { // E1 forward
	out, _ := remediation(t,
		"interface Ethernet1/1\n  shutdown\n",
		"interface Ethernet1/1\n  no shutdown\n")
	assert.Equal(t, "interface Ethernet1/1\n  no shutdown\n", out)
}

func TestDiffToggleReverse(
	t *testing.T,
) { // Exercise the reverse E1 direction.
	out, _ := remediation(t,
		"interface Ethernet1/1\n  no shutdown\n",
		"interface Ethernet1/1\n  shutdown\n")
	assert.Equal(t, "interface Ethernet1/1\n  shutdown\n", out)
	assert.NotContains(t, out, "no no shutdown")
}

func TestDiffKeyChangeIsRemoveAdd(t *testing.T) { // E4
	out, _ := remediation(t, "vlan 10\n", "vlan 20\n")
	assert.Equal(t, "vlan 20\nno vlan 10\n", out)
}

func TestDiffKeptKeyedBodyChange(t *testing.T) { // E5
	out, _ := remediation(t,
		"vlan 10\n  name FOO\n",
		"vlan 10\n  name BAR\n")
	assert.Equal(t, "vlan 10\n  name BAR\n", out)
}

func TestDiffWholeSectionRemove(t *testing.T) { // E7a
	out, _ := remediation(t, "interface Ethernet1/1\n  shutdown\n", "")
	assert.Equal(t, "no interface Ethernet1/1\n", out)
}

func TestDiffKeptSectionAllChildrenRemoved(t *testing.T) { // E7b
	out, _ := remediation(t,
		"interface Ethernet1/1\n  description A\n  shutdown\n",
		"interface Ethernet1/1\n  description A\n")
	assert.Equal(t, "interface Ethernet1/1\n  no shutdown\n", out)
}

func TestDiffAddOrderingDefinitionFirst(t *testing.T) { // E9
	out, _ := remediation(t,
		"",
		"vlan 50\ninterface Ethernet1/1\n  switchport access vlan 50\n")
	assert.Equal(t,
		"vlan 50\ninterface Ethernet1/1\n  switchport access vlan 50\n", out)
}

func TestDiffRemoveOrderingReferrerFirst(
	t *testing.T,
) { // Guard the E10 ordering case.
	out, _ := remediation(t,
		"vlan 50\ninterface Ethernet1/1\n  switchport access vlan 50\n",
		"")
	want := "no interface Ethernet1/1\nno vlan 50\n"
	assert.Equal(t, want, out)
}

func TestDiffDoesNotMutateInputs(t *testing.T) { // aliasing guard
	s := testSchema()
	running := mustParse(t, s, "interface Ethernet1/1\n  description A\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  description B\n")
	_, _ = Diff(running, intended, diag.Policy{})
	assert.Equal(
		t,
		"interface Ethernet1/1\n  description A\n",
		render.Render(running),
	)
	assert.Equal(
		t,
		"interface Ethernet1/1\n  description B\n",
		render.Render(intended),
	)
}

func TestDiffFullLineValueIsAddRemoveNotModify(t *testing.T) { // E6
	out, res := remediation(
		t,
		"interface Ethernet1/1\n  ip address 10.0.0.1 255.255.255.0 secondary\n",
		"interface Ethernet1/1\n  ip address 10.0.0.2 255.255.255.0 secondary\n",
	)
	// a ZeroToN full-line value change is a different line: add new + remove old,
	// never an in-place Modify.
	assert.Equal(t,
		"interface Ethernet1/1\n"+
			"  ip address 10.0.0.2 255.255.255.0 secondary\n"+
			"  no ip address 10.0.0.1 255.255.255.0 secondary\n", out)
	ops := opsByText(res)
	assert.Equal(
		t,
		tree.OpAdd,
		ops["ip address 10.0.0.2 255.255.255.0 secondary"],
	)
	assert.Equal(
		t,
		tree.OpRemove,
		ops["no ip address 10.0.0.1 255.255.255.0 secondary"],
	)
	tree.Walk(res.Tree, func(n *tree.Node) {
		assert.NotEqual(
			t,
			tree.OpModify,
			n.Op,
			"full-line change must not Modify",
		)
	})
}

func TestDiffOpTagsAddRemoveSection(t *testing.T) {
	// Check operation tags and rendered text for add, remove, and section nodes.
	_, res := remediation(t,
		"interface Ethernet1/1\n  shutdown\n",
		"interface Ethernet1/1\n  description NEW\nvlan 99\n")
	ops := opsByText(res)
	assert.Equal(t, tree.OpAdd, ops["vlan 99"])
	assert.Equal(t, tree.OpSection, ops["interface Ethernet1/1"])
	assert.Equal(t, tree.OpAdd, ops["description NEW"])
	assert.Equal(t, tree.OpRemove, ops["no shutdown"]) // shutdown removed
}

func TestDiffUnmatchedNodeDefensive(t *testing.T) { // E11
	s := testSchema()

	// running-only unmatched (def==nil) node => OpRemove "no <text>" + a Warning.
	// Import never produces def==nil nodes, so the tree is hand-built.
	running := tree.NewConfig(s)
	running.Root.AddChild(tree.NewNode("flux-capacitor on"))
	res, d := Diff(running, tree.NewConfig(s), diag.Policy{})
	assert.Equal(t, "no flux-capacitor on\n", render.Render(res.Tree))
	assert.Contains(t, d.String(), "negating unmatched line")

	// intended-only unmatched node => OpAdd verbatim, no warning.
	intended := tree.NewConfig(s)
	intended.Root.AddChild(tree.NewNode("flux-capacitor on"))
	res2, d2 := Diff(tree.NewConfig(s), intended, diag.Policy{})
	assert.Equal(t, "flux-capacitor on\n", render.Render(res2.Tree))
	assert.NotContains(t, d2.String(), "negating unmatched line")
}

func opsByText(res *Result) map[string]tree.Op {
	ops := map[string]tree.Op{}
	tree.Walk(res.Tree, func(n *tree.Node) { ops[n.Text] = n.Op })
	return ops
}

func TestDiffSymmetry(t *testing.T) {
	// Pins the direction-agnostic property Diff(a,b) / Diff(b,a) that
	// Engine.Rollback relies on, at the algorithm level rather than through the
	// facade. E1 already has explicit forward and reverse tests above; E9
	// reversed is E10.
	cases := []struct {
		name       string
		a, b       string
		aToB, bToA string
	}{
		{
			name: "idempotent modify (E2)",
			a:    "interface Ethernet1/1\n  description A\n",
			b:    "interface Ethernet1/1\n  description B\n",
			aToB: "interface Ethernet1/1\n  description B\n",
			bToA: "interface Ethernet1/1\n  description A\n",
		},
		{
			name: "key change (E4)",
			a:    "vlan 10\n",
			b:    "vlan 20\n",
			aToB: "vlan 20\nno vlan 10\n",
			bToA: "vlan 10\nno vlan 20\n",
		},
		{
			name: "keyed body change (E5)",
			a:    "vlan 10\n  name FOO\n",
			b:    "vlan 10\n  name BAR\n",
			aToB: "vlan 10\n  name BAR\n",
			bToA: "vlan 10\n  name FOO\n",
		},
		{
			name: "whole-section remove vs deep re-add (E7a)",
			a:    "interface Ethernet1/1\n  description A\n  shutdown\n",
			b:    "",
			aToB: "no interface Ethernet1/1\n",
			bToA: "interface Ethernet1/1\n  description A\n  shutdown\n",
		},
		{
			name: "keyed churn (add+remove both ways)",
			a:    "vlan 10\nvlan 20\n",
			b:    "vlan 20\nvlan 30\n",
			aToB: "vlan 30\nno vlan 10\n",
			bToA: "vlan 10\nno vlan 30\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fwd, _ := remediation(t, tc.a, tc.b)
			assert.Equal(t, tc.aToB, fwd, "a -> b")
			back, _ := remediation(t, tc.b, tc.a)
			assert.Equal(t, tc.bToA, back, "b -> a")
		})
	}
}

func TestDiffSymmetryOrdering(t *testing.T) {
	// Retarget both ways: the ref-derived edges must reorder each direction so
	// the referrer never points at a vlan that isn't installed yet, and the
	// stale vlan is only negated once nothing still refers to it.
	s := testSchema()
	a := mustParse(t, s,
		"vlan 10\ninterface Ethernet1/1\n  switchport access vlan 10\n")
	b := mustParse(t, s,
		"vlan 20\ninterface Ethernet1/1\n  switchport access vlan 20\n")

	fwd, d := Diff(a, b, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(
		t,
		"vlan 20\ninterface Ethernet1/1\n  switchport access vlan 20\nno vlan 10\n",
		render.Render(fwd.Tree),
	)

	rev, d2 := Diff(b, a, diag.Policy{Strict: true})
	require.False(t, d2.HasErrors(), d2.String())
	assert.Equal(
		t,
		"vlan 10\ninterface Ethernet1/1\n  switchport access vlan 10\nno vlan 20\n",
		render.Render(rev.Tree),
	)
}

// headerFieldSchema: a keyed SECTION whose header carries a non-key field.
func headerFieldSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	vlan := s.Node("vlan {{ id:vlan }} name {{ name:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	vlan.Child("shutdown").Card(schema.ZeroToOne)
	return s
}

func TestDiffSectionHeaderFieldChange(t *testing.T) {
	// A changed non-key section header must emit OpModify and a change-log entry.
	s := headerFieldSchema()
	running := mustParse(t, s, "vlan 10 name FOO\n")
	intended := mustParse(t, s, "vlan 10 name BAR\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.False(t, res.Empty())
	assert.Equal(t, "vlan 10 name BAR\n", render.Render(res.Tree))
	assert.Equal(t, tree.OpModify, opsByText(res)["vlan 10 name BAR"])
	// The change log pairs both headers, so compare can show - old / + new.
	require.Len(t, res.Changes, 1)
	c := res.Changes[0]
	assert.Equal(t, graph.Modify, c.Action)
	require.NotNil(t, c.Running)
	require.NotNil(t, c.Intended)
	assert.Equal(t, "vlan 10 name FOO", c.Running.Text)
	assert.Equal(t, "vlan 10 name BAR", c.Intended.Text)
}

func TestDiffSectionHeaderFieldChangeWithChildChange(t *testing.T) {
	// Emit the header change first, then re-enter the section for child operations.
	s := headerFieldSchema()
	running := mustParse(t, s, "vlan 10 name FOO\n  shutdown\n")
	intended := mustParse(t, s, "vlan 10 name BAR\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"vlan 10 name BAR\nvlan 10 name BAR\n  no shutdown\n",
		render.Render(res.Tree))
}

func TestDiffSectionHeaderUnchangedNoModify(t *testing.T) {
	// An unchanged header must not produce a modification.
	s := headerFieldSchema()
	running := mustParse(t, s, "vlan 10 name FOO\n  shutdown\n")
	intended := mustParse(t, s, "vlan 10 name FOO\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan 10 name FOO\n  no shutdown\n",
		render.Render(res.Tree))
	require.Len(t, res.Changes, 1)
	assert.Equal(t, graph.Remove, res.Changes[0].Action)
}

func TestDiffDuplicateRunningIdentWarns(t *testing.T) {
	// Two running lines with one ident: pairing sees only the first, the
	// second is untouchable by Diff (it pairs with nothing and is not
	// removed). That stays un-remediated by design, but never silent.
	s := testSchema()
	running := mustParse(t, s, "vlan 10\nvlan 10\n")
	intended := mustParse(t, s, "vlan 10\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty()) // surfaced, not remediated
	assert.Contains(t, d.String(), "duplicate")
	assert.Contains(t, d.String(), "vlan 10")
}

func TestDiffDuplicateIdempotentRunningValueWarns(t *testing.T) {
	// Warn when a duplicate running slot leaves an unpaired value that remediation cannot remove.
	s := testSchema()
	running := mustParse(t, s,
		"interface Ethernet1/1\n  description A\n  description B\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  description A\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Contains(t, d.String(), "duplicate")
	assert.Contains(t, d.String(), "description B")
}

func TestResultEmptyUnknownOpIsNotEmpty(t *testing.T) {
	// A future tree.Op value must never read as "no change".
	out := tree.NewConfig(testSchema())
	n := tree.NewNode("mystery line")
	n.Op = tree.Op(99)
	out.Root.AddChild(n)
	r := &Result{Tree: out}
	assert.False(t, r.Empty())
}

func TestDiffSchemaMismatch(t *testing.T) {
	running := mustParse(t, testSchema(), "vlan 10\n")
	intended := mustParse(
		t,
		testSchema(),
		"vlan 10\n",
	) // different *Schema instance
	res, d := Diff(running, intended, diag.Policy{})
	assert.True(t, d.HasErrors())
	assert.True(t, res.Empty())
}

func bannerSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		BlockDelim("delim").NegateAs("no banner motd")
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	return s
}

func TestDiffBlockBodyChangeIsModify(t *testing.T) {
	s := bannerSchema()
	run := mustParse(t, s, "banner motd ^\nold body\n^\n")
	intd := mustParse(t, s, "banner motd ^\nnew body\n^\n")
	res, d := Diff(run, intd, diag.Policy{})
	require.False(t, d.HasErrors())
	assert.Equal(t, "banner motd ^\nnew body\n^\n", render.Render(res.Tree))
	assert.Equal(t, tree.OpModify, res.Tree.Root.Children[0].Op)
}

func TestDiffBlockIdenticalIsNoop(t *testing.T) {
	s := bannerSchema()
	run := mustParse(t, s, "banner motd ^\nsame\n^\n")
	intd := mustParse(t, s, "banner motd ^\nsame\n^\n")
	res, _ := Diff(run, intd, diag.Policy{})
	assert.True(t, res.Empty())
}

func TestDiffBlockAddAndRemove(t *testing.T) {
	s := bannerSchema()
	res, _ := Diff(
		mustParse(t, s, ""),
		mustParse(t, s, "banner motd ^\nhi\n^\n"),
		diag.Policy{},
	)
	assert.Equal(
		t,
		"banner motd ^\nhi\n^\n",
		render.Render(res.Tree),
	) // add carries body

	res2, _ := Diff(
		mustParse(t, s, "banner motd ^\nhi\n^\n"),
		mustParse(t, s, ""),
		diag.Policy{},
	)
	assert.Equal(
		t,
		"no banner motd\n",
		render.Render(res2.Tree),
	) // NegateAs, no body
}

func keyedLeafSchema(idempotent bool) *schema.Schema {
	s := schema.New()
	n := s.Node("ip route {{ prefix:word }} via {{ nh:word }}").
		Card(schema.ZeroToN).Key("prefix")
	if idempotent {
		n.MarkIdempotent()
	}
	return s
}

func TestDiffKeyedLeafIdempotentIsModify(t *testing.T) {
	s := keyedLeafSchema(true)
	run := mustParse(t, s, "ip route 10.0.0.0/8 via 1.1.1.1\n")
	intd := mustParse(t, s, "ip route 10.0.0.0/8 via 2.2.2.2\n")
	res, d := Diff(run, intd, diag.Policy{})
	require.False(t, d.HasErrors())
	assert.Equal(
		t,
		"ip route 10.0.0.0/8 via 2.2.2.2\n",
		render.Render(res.Tree),
	)
	assert.Equal(t, tree.OpModify, res.Tree.Root.Children[0].Op)
}

func TestDiffKeyedLeafReplacePair(t *testing.T) {
	s := keyedLeafSchema(false)
	run := mustParse(t, s, "ip route 10.0.0.0/8 via 1.1.1.1\n")
	intd := mustParse(t, s, "ip route 10.0.0.0/8 via 2.2.2.2\n")
	res, d := Diff(run, intd, diag.Policy{})
	require.False(t, d.HasErrors())
	// negate-first: never two values for one key on-device
	assert.Equal(t,
		"no ip route 10.0.0.0/8 via 1.1.1.1\nip route 10.0.0.0/8 via 2.2.2.2\n",
		render.Render(res.Tree))
	top := res.Tree.Root.Children
	require.Len(t, top, 2)
	assert.Equal(t, tree.OpRemove, top[0].Op)
	assert.Equal(t, tree.OpAdd, top[1].Op)
}

func TestDiffKeyedLeafUnchangedIsNoop(t *testing.T) {
	s := keyedLeafSchema(false)
	run := mustParse(t, s, "ip route 10.0.0.0/8 via 1.1.1.1\n")
	intd := mustParse(t, s, "ip route 10.0.0.0/8 via 1.1.1.1\n")
	res, _ := Diff(run, intd, diag.Policy{})
	assert.True(t, res.Empty())
}

func TestDiffKeyedLeafSymmetry(t *testing.T) {
	// rollback = Diff(intended, running): Modify reverses to the old value,
	// the replace pair reverses to negate-new + add-old.
	run := "ip route 10.0.0.0/8 via 1.1.1.1\n"
	intd := "ip route 10.0.0.0/8 via 2.2.2.2\n"

	s := keyedLeafSchema(true)
	back, _ := Diff(
		mustParse(t, s, intd),
		mustParse(t, s, run),
		diag.Policy{},
	)
	assert.Equal(
		t,
		"ip route 10.0.0.0/8 via 1.1.1.1\n",
		render.Render(back.Tree),
	)

	s2 := keyedLeafSchema(false)
	back2, _ := Diff(
		mustParse(t, s2, intd),
		mustParse(t, s2, run),
		diag.Policy{},
	)
	assert.Equal(t,
		"no ip route 10.0.0.0/8 via 2.2.2.2\nip route 10.0.0.0/8 via 1.1.1.1\n",
		render.Render(back2.Tree))
}

func TestDiffKeyedCrossDefPairs(t *testing.T) {
	// Pair sibling templates with the same Kind and key so a rename emits Modify.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id").MarkIdempotent()
	s.Node("vlan {{ id:vlan }} name {{ name:word }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id").MarkIdempotent()
	run := mustParse(t, s, "vlan 10 state enable\n")
	intd := mustParse(t, s, "vlan 10 name CORE state enable\n")
	res, d := Diff(run, intd, diag.Policy{})
	require.False(t, d.HasErrors())
	out := render.Render(res.Tree)
	assert.Equal(t, "vlan 10 name CORE state enable\n", out)
	assert.NotContains(t, out, "no vlan")
}

func TestDiffFullLineBlockBodyIsReplacePair(t *testing.T) {
	// Replace a changed non-idempotent block by removing the old body before adding the new body.
	s := schema.New()
	s.Node("certificate {{ name:word }}").
		Card(schema.ZeroToN).
		BlockUntil("quit")
	run := mustParse(t, s, "certificate ca1\nbodyA\nquit\n")
	intd := mustParse(t, s, "certificate ca1\nbodyB\nquit\n")
	res, d := Diff(run, intd, diag.Policy{})
	require.False(t, d.HasErrors())
	assert.Equal(t,
		"no certificate ca1\ncertificate ca1\nbodyB\nquit\n",
		render.Render(res.Tree))
}

func TestDiffKeyedBlockBodyChange(t *testing.T) {
	// Keyed block node with a body-only change must not be a silent no-op.
	s := schema.New()
	s.Node("certificate {{ name:word }}").
		Card(schema.ZeroToN).Key("name").BlockUntil("quit")
	run := mustParse(t, s, "certificate ca1\nbodyA\nquit\n")
	intd := mustParse(t, s, "certificate ca1\nbodyB\nquit\n")
	res, d := Diff(run, intd, diag.Policy{})
	require.False(t, d.HasErrors())
	assert.False(t, res.Empty())
	assert.Equal(t,
		"no certificate ca1\ncertificate ca1\nbodyB\nquit\n",
		render.Render(res.Tree))
}

func TestDiffDeterministic(t *testing.T) {
	running := "vlan 10\ninterface Ethernet1/1\n  description A\n  shutdown\n"
	intended := "vlan 20\ninterface Ethernet1/1\n  description B\n  switchport access vlan 20\n"
	first, _ := remediation(t, running, intended)
	for range 5 {
		out, _ := remediation(t, running, intended)
		assert.Equal(t, first, out)
	}
}

func TestDiffReplacesKeyedSectionWhenDefChanges(t *testing.T) {
	s := schema.New()
	a := s.Node("mode {{ id:word }} a").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	a.Child("a-child").Card(schema.ZeroToOne)
	b := s.Node("mode {{ id:word }} b").Card(schema.ZeroToN).
		Kind("mode").Key("id")
	b.Child("b-child").Card(schema.ZeroToOne)
	running := mustParse(t, s, "mode 1 a\n  a-child\n")
	intended := mustParse(t, s, "mode 1 b\n  b-child\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(
		t,
		"no mode 1 a\nmode 1 b\n  b-child\n",
		render.Render(res.Tree),
	)
	require.Len(t, res.Changes, 1)
	assert.Equal(t, graph.Replace, res.Changes[0].Action)
}

func TestToggleFlipOldRefOrdersBeforeTargetRemoval(t *testing.T) {
	s := schema.New()
	targets := s.Node("targets").Card(schema.ZeroToOne)
	targets.Child("target {{ id:word }}").Card(schema.ZeroToN).
		Kind("target").Key("id")
	features := s.Node("features").Card(schema.ZeroToOne)
	on := features.Child("feature on").Card(schema.ZeroToOne)
	on.Child("use {{ id:word }}").Card(schema.ZeroToOne).
		Ref("id", "target.id")
	features.Child("feature off").Card(schema.ZeroToOne).Toggles(on)
	running := mustParse(t, s,
		"targets\n  target 1\nfeatures\n  feature on\n    use 1\n")
	intended := mustParse(t, s, "targets\nfeatures\n  feature off\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Less(
		t,
		strings.Index(out, "feature off"),
		strings.Index(out, "no target 1"),
	)
}

func TestToggleFlipOldRequiresOrdersBeforeTargetRemoval(t *testing.T) {
	s := schema.New()
	targets := s.Node("targets").Card(schema.ZeroToOne)
	targets.Child("target").Card(schema.ZeroToOne).Kind("target")
	features := s.Node("features").Card(schema.ZeroToOne)
	on := features.Child("feature on").Card(schema.ZeroToOne).Requires("target")
	features.Child("feature off").Card(schema.ZeroToOne).Toggles(on)
	running := mustParse(t, s, "targets\n  target\nfeatures\n  feature on\n")
	intended := mustParse(t, s, "targets\nfeatures\n  feature off\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Less(
		t,
		strings.Index(out, "feature off"),
		strings.Index(out, "no target"),
	)
}

func TestToggleFlipFreesOldExclusiveResourceBeforeClaim(t *testing.T) {
	s := schema.New()
	destination := s.Node("destination").Card(schema.ZeroToOne)
	claim := destination.Child("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id")
	features := s.Node("features").Card(schema.ZeroToOne)
	on := features.Child("feature on").Card(schema.ZeroToOne)
	on.Adopt(claim)
	features.Child("feature off").Card(schema.ZeroToOne).Toggles(on)
	running := mustParse(
		t,
		s,
		"destination\nfeatures\n  feature on\n    claim 1\n",
	)
	intended := mustParse(
		t,
		s,
		"destination\n  claim 1\nfeatures\n  feature off\n",
	)
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Less(
		t,
		strings.Index(out, "feature off"),
		strings.Index(out, "claim 1"),
	)
}

func TestDiffUndeclaredTogglePairEmitsBoth(t *testing.T) {
	// Without a Toggles declaration the flip is two independent changes:
	// remove "shutdown" (negates to "no shutdown") + add "no shutdown".
	// The old text heuristic would have deduped this; declared pairs are
	// the only toggle knowledge now.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("no shutdown").Card(schema.ZeroToOne)
	running := mustParse(t, s, "interface Ethernet1/1\n  shutdown\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  no shutdown\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Equal(t, 2, strings.Count(out, "no shutdown"))
}

// duplexSchema declares a 3-way toggle group: three spellings of one switch.
func duplexSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	auto := iface.Child("duplex auto").Card(schema.ZeroToOne)
	full := iface.Child("duplex full").Card(schema.ZeroToOne)
	iface.Child("duplex half").Card(schema.ZeroToOne).Toggles(auto, full)
	return s
}

func TestDiffToggleGroupThreeWayFlip(t *testing.T) {
	// full -> half is a flip within the group: one forward line, no negate.
	s := duplexSchema()
	running := mustParse(t, s, "interface Ethernet1/1\n  duplex full\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  duplex half\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Equal(t, "interface Ethernet1/1\n  duplex half\n", out)
	assert.NotContains(t, out, "no duplex")
	// The Modify change records the superseded running line.
	require.Len(t, res.Changes, 1)
	assert.NotNil(t, res.Changes[0].Running)
	assert.NotNil(t, res.Changes[0].Intended)
}

func TestDiffToggleGroupThreeWayReverse(t *testing.T) {
	// The mirror direction (half -> full): flip detection must be symmetric
	// across ANY ordered pair of group members, not just the declared order.
	s := duplexSchema()
	running := mustParse(t, s, "interface Ethernet1/1\n  duplex half\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  duplex full\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Equal(t, "interface Ethernet1/1\n  duplex full\n", out)
	assert.NotContains(t, out, "no duplex")
}

func TestDiffToggleGroupIllegalDoubleRunning(t *testing.T) {
	// Converge invalid input with two toggle members by emitting one forward line and recording the first superseded member.
	s := duplexSchema()
	running := mustParse(t, s,
		"interface Ethernet1/1\n  duplex auto\n  duplex full\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  duplex half\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "duplex half")
	assert.NotContains(t, out, "no duplex")
	// The second superseded member is dropped from the artifact by design,
	// but never silently: the warning names it and the paired first member.
	assert.Contains(t, d.String(), "multiple members of toggle group")
	assert.Contains(t, d.String(), `"duplex full"`) // the second, unpaired one
	assert.Contains(t, d.String(), `"duplex auto"`) // what the log pairs
	// Pair the first superseded member with the flip.
	require.Len(t, res.Changes, 1)
	require.NotNil(t, res.Changes[0].Running)
	assert.Equal(t, "duplex auto", res.Changes[0].Running.Text)
}

func TestDiffCustomNegationWord(t *testing.T) {
	// Schema-wide negation word (hier_config precedent): removals render as
	// "<word> <line>", and a line already starting with "<word> " strips it.
	s := schema.New().Negation("undo")
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	iface.Child("undo logging").Card(schema.ZeroToOne)
	running := mustParse(t, s,
		"interface Ethernet1/1\n  description X\n  undo logging\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "undo description X") // prepend form
	assert.Contains(t, out, "\n  logging\n")      // strip form
	assert.NotContains(t, out, "no ")             // the old hardcoded word
}

// protectedSchema: a protected ZeroToOne section with a child, an unprotected
// sibling leaf, and a protected toggle pair.
func protectedSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	bgp := s.Node("router bgp {{ asn:asn }}").Card(schema.ZeroToOne).Protect()
	bgp.Child("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN)
	s.Node("logging on").Card(schema.ZeroToOne)
	shut := s.Node("shutdown").Card(schema.ZeroToOne).Protect()
	s.Node("no shutdown").Card(schema.ZeroToOne).Toggles(shut)
	return s
}

func TestDiffProtectedSectionRefusesBothPolicies(t *testing.T) {
	s := protectedSchema()
	running := mustParse(t, s,
		"router bgp 65000\n  neighbor 10.0.0.1 remote-as 65001\nlogging on\n")
	intended := mustParse(t, s, "logging on\n")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to delete protected")
		out := render.Render(res.Tree)
		// Suppress the complete protected subtree.
		assert.NotContains(t, out, "no router bgp")
		assert.NotContains(t, out, "no neighbor")
	}
}

func TestDiffProtectedIdentityChangeRefuses(t *testing.T) {
	// A ZeroToOne slot value change is remove-old + add-new; the remove half
	// trips the rail, so an ASN migration always fails to auto-generate.
	s := protectedSchema()
	running := mustParse(t, s, "router bgp 65000\n")
	intended := mustParse(t, s, "router bgp 65001\n")
	_, d := Diff(running, intended, diag.Policy{Strict: false})
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "refusing to delete protected")
}

func TestDiffProtectedToggleFlipIsNotDeletion(t *testing.T) {
	// Toggling a protected line to its declared partner is a value change:
	// the dedup drops the remove before the protection check ever sees it.
	s := protectedSchema()
	running := mustParse(t, s, "shutdown\n")
	intended := mustParse(t, s, "no shutdown\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "no shutdown\n", render.Render(res.Tree))
}

func TestDiffProtectedReplacePairRefuses(t *testing.T) {
	// A keyed non-idempotent value change is a replace pair (negate + add);
	// the negate half is a deletion, so a protected def refuses the whole op.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN).Key("peer").Protect()
	running := mustParse(t, s, "neighbor 10.0.0.1 remote-as 65001\n")
	intended := mustParse(t, s, "neighbor 10.0.0.1 remote-as 65002\n")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to replace protected")
		out := render.Render(res.Tree)
		// Suppress both halves because a lone add can duplicate lines.
		assert.NotContains(t, out, "no neighbor")
		assert.NotContains(t, out, "remote-as 65002")
	}
}

func TestDiffProtectedDescendantInRemovedSectionRefuses(t *testing.T) {
	// Removing an unprotected section deletes its whole subtree; a protected
	// descendant inside it must trip the rail even though the header is plain.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("critical monitor").Card(schema.ZeroToOne).Protect()
	running := mustParse(t, s, "interface Ethernet1/1\n  critical monitor\n")
	intended := mustParse(t, s, "")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to delete protected")
		out := render.Render(res.Tree)
		assert.NotContains(t, out, "no interface")
		assert.NotContains(t, out, "critical monitor")
	}
}

func TestDiffProtectedModifyAllowed(t *testing.T) {
	// Permit an idempotent protected value change because it emits no deletion.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).MarkIdempotent().Protect()
	running := mustParse(t, s, "interface Ethernet1/1\n  description A\n")
	intended := mustParse(t, s, "interface Ethernet1/1\n  description B\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Contains(t, render.Render(res.Tree), "description B")
}

func TestDiffProtectedReplaceKindUnifiedAsymmetric(t *testing.T) {
	// Refuse replacement when only the running definition in a Kind-paired node is protected.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN).Kind("nbr").Key("peer").Protect()
	s.Node("neighbor {{ peer:ipv4 }} description {{ text:rest }}").
		Card(schema.ZeroToN).Kind("nbr").Key("peer")
	running := mustParse(t, s, "neighbor 10.0.0.1 remote-as 65001\n")
	intended := mustParse(t, s, "neighbor 10.0.0.1 description foo\n")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to replace protected")
		out := render.Render(res.Tree)
		assert.NotContains(t, out, "no neighbor")
		assert.NotContains(t, out, "description foo")
	}
}

func TestDiffProtectedDescendantInToggleFlipRefuses(t *testing.T) {
	// Refuse a toggle flip that would remove a protected descendant with the old toggle subtree.
	s := schema.New()
	on := s.Node("feature on").Card(schema.ZeroToOne)
	s.Node("feature off").Card(schema.ZeroToOne).Toggles(on)
	on.Child("critical monitor").Card(schema.ZeroToOne).Protect()
	running := mustParse(t, s, "feature on\n  critical monitor\n")
	intended := mustParse(t, s, "feature off\n")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to delete protected")
		out := render.Render(res.Tree)
		assert.NotContains(t, out, "critical monitor")
	}
}

func TestDiffProtectedRollbackDirectionRefuses(t *testing.T) {
	// Diff is direction-agnostic; a rollback that deletes protected refuses too.
	s := protectedSchema()
	running := mustParse(t, s, "logging on\n")
	intended := mustParse(t, s, "router bgp 65000\nlogging on\n")
	// Forward adds the protected section (fine)…
	_, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	// …its rollback (reversed args) deletes it: refused.
	_, rd := Diff(intended, running, diag.Policy{Strict: true})
	assert.True(t, rd.HasErrors())
	assert.Contains(t, rd.String(), "refusing to delete protected")
}

// emptyOnRemoveSchema: a mode-like ZeroToOne section declared EmptyOnRemove
// (a "vlan database"-style mode) with keyed children and a cross-section
// referrer.
func emptyOnRemoveSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database").Card(schema.ZeroToOne).ClearOnRemove()
	vdb.Child("vlan {{ id:vlan }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id").
		NegateAs("no vlan {{ id }}")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().Ref("vlan", "vlan.id")
	return s
}

func TestDiffEmptyOnRemoveSectionNegatesChildren(t *testing.T) { // E7c
	s := emptyOnRemoveSchema()
	running := mustParse(t, s, "vlan database\n  vlan 10\n  vlan 20\n")
	intended := mustParse(t, s, "")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Equal(t, "vlan database\n  no vlan 20\n  no vlan 10\n", out)
	assert.NotContains(t, out, "no vlan database")
}

func TestDiffEmptyOnRemoveEmptySectionIsNoop(t *testing.T) {
	// Removing an empty mode requires no operation or header output.
	s := emptyOnRemoveSchema()
	running := mustParse(t, s, "vlan database\n")
	intended := mustParse(t, s, "")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestDiffEmptyOnRemoveProtectedChildPartial(t *testing.T) {
	// The Protected rail is per child in an expansion: the protected subtree
	// refuses with an Error, its siblings still negate. (Header negation is
	// all-or-nothing only because one line deletes everything at once.)
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database").Card(schema.ZeroToOne).ClearOnRemove()
	vdb.Child("vlan {{ id:vlan }}").Card(schema.ZeroToN).Key("id").
		NegateAs("no vlan {{ id }}")
	vdb.Child("critical monitor").Card(schema.ZeroToOne).Protect()
	running := mustParse(t, s,
		"vlan database\n  vlan 10\n  critical monitor\n")
	intended := mustParse(t, s, "")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to delete protected")
		out := render.Render(res.Tree)
		assert.Contains(t, out, "no vlan 10")
		assert.NotContains(t, out, "critical monitor")
		assert.NotContains(t, out, "no vlan database")
	}
}

func TestDiffEmptyOnRemoveProtectedDescendantRefuses(t *testing.T) {
	// Recursively refuse an expanded child subtree with a protected descendant while removing unprotected siblings.
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database").Card(schema.ZeroToOne).ClearOnRemove()
	vdb.Child("vlan {{ id:vlan }}").Card(schema.ZeroToN).Key("id").
		NegateAs("no vlan {{ id }}")
	grp := vdb.Child("group {{ id:vlan }}").Card(schema.ZeroToN).Key("id")
	grp.Child("critical monitor").Card(schema.ZeroToOne).Protect()
	running := mustParse(t, s,
		"vlan database\n  vlan 10\n  group 5\n    critical monitor\n")
	intended := mustParse(t, s, "")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "refusing to delete protected")
		assert.Contains(t, d.String(), "critical monitor")
		out := render.Render(res.Tree)
		assert.NotContains(t, out, "no group 5") // protected subtree refused
		assert.NotContains(t, out, "critical monitor")
		assert.Contains(t, out, "no vlan 10") // sibling still negates
	}
}

func TestDiffEmptyOnRemoveNestedSectionHeaderNegated(t *testing.T) {
	// A child section WITHOUT EmptyOnRemove inside an expanded one keeps the
	// ordinary one-line header negation.
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database").Card(schema.ZeroToOne).ClearOnRemove()
	grp := vdb.Child("group {{ id:vlan }}").Card(schema.ZeroToN).Key("id")
	grp.Child("member {{ id:vlan }}").Card(schema.ZeroToN).Key("id")
	running := mustParse(t, s, "vlan database\n  group 5\n    member 1\n")
	intended := mustParse(t, s, "")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan database\n  no group 5\n",
		render.Render(res.Tree))
}

func TestDiffEmptyOnRemoveChangesPerChild(t *testing.T) {
	// Record one Change per removed child with the section path and no header Change.
	s := emptyOnRemoveSchema()
	running := mustParse(t, s, "vlan database\n  vlan 10\n  vlan 20\n")
	intended := mustParse(t, s, "")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, res.Changes, 2)
	for _, c := range res.Changes {
		assert.Equal(t, graph.Remove, c.Action)
		assert.Nil(t, c.Intended)
		require.NotNil(t, c.Running)
		assert.Equal(t, []string{"vlan database"}, c.Path)
	}
	assert.Equal(t, "vlan 20", res.Changes[0].Running.Text)
	assert.Equal(t, "vlan 10", res.Changes[1].Running.Text)
}

func TestDiffEmptyOnRemoveRefOrderingAcrossSections(t *testing.T) {
	// A referrer in another section still negates before the expanded child
	// that defines the target: each child op carries its own refs.
	s := emptyOnRemoveSchema()
	running := mustParse(
		t,
		s,
		"vlan database\n  vlan 10\ninterface Ethernet1/1\n  switchport access vlan 10\n",
	)
	intended := mustParse(t, s, "interface Ethernet1/1\n")
	res, d := Diff(running, intended, diag.Policy{Strict: true})
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Less(t,
		strings.Index(out, "no switchport access vlan 10"),
		strings.Index(out, "no vlan 10"), out)
	assert.NotContains(t, out, "no vlan database")
}

func TestDiffEmptyOnRemoveReplacePairRefuses(t *testing.T) {
	// Refuse ckReplace when a Kind-paired running EmptyOnRemove section has no header removal form.
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database {{ id:vlan }}").
		Card(schema.ZeroToN).Kind("vdb").Key("id").ClearOnRemove()
	vdb.Child("member {{ m:vlan }}").Card(schema.ZeroToN).Key("m")
	s.Node("vlan database {{ id:vlan }} disabled").
		Card(schema.ZeroToN).Kind("vdb").Key("id")
	running := mustParse(t, s, "vlan database 5\n  member 1\n")
	intended := mustParse(t, s, "vlan database 5 disabled\n")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "no removal form")
		out := render.Render(res.Tree)
		// Suppress both halves because a lone add can duplicate configuration.
		assert.NotContains(t, out, "no vlan database")
		assert.NotContains(t, out, "disabled")
	}
}

func TestDiffEmptyOnRemoveChildlessDefErrors(t *testing.T) {
	// EmptyOnRemove on a def with no child grammar can never converge: the
	// removal would silently no-op forever. No schema build step exists, so
	// it reports at Diff time (the Requires derivation-Error precedent).
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("feature lldp").Card(schema.ZeroToOne).ClearOnRemove()
	running := mustParse(t, s, "feature lldp\n")
	intended := mustParse(t, s, "")
	for _, strict := range []bool{true, false} {
		res, d := Diff(running, intended, diag.Policy{Strict: strict})
		assert.True(t, d.HasErrors(), "strict=%v must Error", strict)
		assert.Contains(t, d.String(), "cannot converge")
		assert.Equal(t, "", render.Render(res.Tree))
	}
}
