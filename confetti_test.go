package confetti_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/fixture/alpha"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

const cleanConfig = "vlan 10\n" +
	"  name USERS\n" +
	"interface Ethernet1/1\n" +
	"  switchport mode access\n" +
	"  switchport access vlan 10\n" +
	"  no shutdown\n"

func TestE2ECanonicalRoundTrip(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	cfg, d := e.Import(cleanConfig)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Equal(t, cleanConfig, out)
	// canonicalization is idempotent
	cfg2, _ := e.Import(out)
	out2, _ := e.Render(cfg2)
	assert.Equal(t, cleanConfig, out2)
}

func TestE2EDanglingRefThenFixed(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})

	bad := "interface Ethernet1/1\n  switchport access vlan 99\n"
	cfg, d := e.Import(bad)
	require.False(t, d.HasErrors(), d.String())
	cc := e.CommitCheck(cfg)
	require.True(t, cc.HasErrors())
	assert.Contains(t, cc.String(), `vlan "99" does not exist`)

	good := "vlan 99\n  name TEST\n" + bad
	cfg2, d2 := e.Import(good)
	require.False(t, d2.HasErrors(), d2.String())
	assert.False(t, e.CommitCheck(cfg2).HasErrors())
}

func TestE2EBrownfieldLenientVsStrict(t *testing.T) {
	in := "interface Ethernet1/1\n" +
		"  flux-capacitor enable\n" +
		"  no shutdown\n"

	lenient := alpha.Engine(diag.Policy{Strict: false})
	cfg, d := lenient.Import(in)
	assert.False(t, d.HasErrors()) // warnings only
	assert.NotEmpty(t, d.Items)    // unsupported-command warning present
	out, _ := lenient.Render(cfg)
	assert.NotContains(t, out, "flux-capacitor") // dropped from tree
	assert.Contains(t, out, "no shutdown")       // known command survives

	strict := alpha.Engine(diag.Policy{Strict: true})
	_, ds := strict.Import(in)
	assert.True(t, ds.HasErrors()) // same input is a hard error under Strict
}

func remediateSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().Ref("vlan", "vlan.id")
	return s
}

func TestEngineRemediateCommitChecksIntended(t *testing.T) { // E12
	e := confetti.New(
		remediateSchema(),
		confetti.WithPolicy(diag.Policy{Strict: true}),
	)
	running, d1 := e.Import("")
	require.False(t, d1.HasErrors())
	// intended references a vlan that is never defined
	intended, d2 := e.Import(
		"interface Ethernet1/1\n  switchport access vlan 99\n",
	)
	require.False(
		t,
		d2.HasErrors(),
	) // parse is fine; the ref is a commit-check concern

	res, d := e.Remediate(running, intended)
	assert.True(
		t,
		d.HasErrors(),
		"remediating onto a dangling-ref goal must error",
	)
	assert.Contains(t, d.String(), `vlan "99" does not exist`)
	// the Result is still returned for inspection
	require.NotNil(t, res)
	assert.NotEqual(t, "", render.Render(res.Tree))
}

func TestEngineRemediateClean(t *testing.T) {
	e := confetti.New(
		remediateSchema(),
		confetti.WithPolicy(diag.Policy{Strict: true}),
	)
	running, _ := e.Import("vlan 10\n")
	intended, _ := e.Import("vlan 10\nvlan 20\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan 20\n", render.Render(res.Tree))
}

func TestRemediateAlphaEndToEnd(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})

	running, dr := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	require.False(t, dr.HasErrors(), dr.String())

	intended, di := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)

	// vlan 20 created before the interface that references it (E9);
	// vlan 10 negated after the interface stanza that referenced it (E10).
	assert.Equal(t,
		"vlan 20\n"+
			"interface Ethernet1/1\n"+
			"  switchport access vlan 20\n"+
			"  no shutdown\n"+
			"no vlan 10\n",
		out)
}

func TestRemediateAlphaAddressFamilyExit(t *testing.T) { // E8
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("feature bgp\nrouter bgp 65001\n")
	intended, _ := e.Import(
		"feature bgp\n" +
			"route-map RM permit 10\n" +
			"router bgp 65001\n" +
			"  address-family ipv4 unicast\n" +
			"    neighbor 10.0.0.1 route-map RM in\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "exit-address-family")
	// route-map defined before the bgp stanza that references it
	assert.Less(
		t,
		indexOf(out, "route-map RM permit 10"),
		indexOf(out, "router bgp 65001"),
	)
}

func TestRemediateAlphaIdempotent(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	intended, _ := e.Import(
		"vlan 10\ninterface Ethernet1/1\n  switchport access vlan 10\n",
	)
	res, d := e.Remediate(intended, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestRemediateAlphaRemovedAddressFamilyNoExit(
	t *testing.T,
) { // E8 (removal side)
	e := alpha.Engine(diag.Policy{Strict: true})
	running, dr := e.Import(
		"feature bgp\nrouter bgp 65001\n" +
			"  address-family ipv4 unicast\n" +
			"    neighbor 10.0.0.1 activate\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import("feature bgp\nrouter bgp 65001\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)

	// the AF is negated as a single header line, with NO exit token
	assert.Contains(t, out, "no address-family ipv4 unicast")
	assert.NotContains(t, out, "exit-address-family")
}

func TestRemediateAlphaSafilessAddressFamilyExit(t *testing.T) { // E8 SAFI-less
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("feature bgp\nrouter bgp 65001\n")
	intended, di := e.Import(
		"feature bgp\nrouter bgp 65001\n" +
			"  address-family ipv4\n" + // SAFI-less Adopt'd form
			"    neighbor 10.0.0.1 activate\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	// the added SAFI-less AF still fires its section-exit token
	assert.Contains(t, out, "address-family ipv4\n")
	assert.Contains(t, out, "exit-address-family")
}

func TestRemediateAlphaRemovedSafilessAddressFamilyNoExit(t *testing.T) { // E8
	e := alpha.Engine(diag.Policy{Strict: true})
	running, dr := e.Import(
		"feature bgp\nrouter bgp 65001\n" +
			"  address-family ipv4\n" +
			"    neighbor 10.0.0.1 activate\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, _ := e.Import("feature bgp\nrouter bgp 65001\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	// removed AF is negated as a single header line, with NO exit token
	assert.Contains(t, out, "no address-family ipv4")
	assert.NotContains(t, out, "exit-address-family")
}

func TestRemediateAlphaFeatureBeforeRouterBGP(t *testing.T) {
	// Requires("feature-bgp") lowers to graph edges: the feature add precedes
	// router bgp on create; teardown negates router bgp before the feature.
	e := alpha.Engine(diag.Policy{Strict: true})
	empty, _ := e.Import("")
	full, d := e.Import("feature bgp\nrouter bgp 65001\n")
	require.False(t, d.HasErrors(), d.String())

	res, rd := e.Remediate(empty, full)
	require.False(t, rd.HasErrors(), rd.String())
	out := render.Render(res.Tree)
	assert.Less(t, indexOf(out, "feature bgp"), indexOf(out, "router bgp"))

	// Rollback must refuse to remove protected router BGP.
	_, rb := e.Rollback(empty, full)
	assert.True(t, rb.HasErrors(), "protected router bgp must refuse teardown")
}

func indexOf(haystack, needle string) int {
	return strings.Index(haystack, needle)
}

// engineOpsByText indexes facade results by operation text.
func engineOpsByText(res *remediate.Result) map[string]tree.Op {
	ops := map[string]tree.Op{}
	tree.Walk(res.Tree, func(n *tree.Node) { ops[n.Text] = n.Op })
	return ops
}

func TestRollbackAlphaInvertsRemediate(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, dr := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	// The exact inverse of TestRemediateAlphaEndToEnd, with E9/E10 ordering
	// holding in this direction too: vlan 10 defined before the interface that
	// references it, vlan 20 negated after (trailing removes group).
	assert.Equal(t,
		"vlan 10\n"+
			"interface Ethernet1/1\n"+
			"  switchport access vlan 10\n"+
			"  shutdown\n"+
			"no vlan 20\n",
		render.Render(res.Tree))
}

func TestRollbackOpsMirrorRemediate(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	intended, _ := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")

	fwd, _ := e.Remediate(running, intended)
	back, _ := e.Rollback(running, intended)
	fops := engineOpsByText(fwd)
	bops := engineOpsByText(back)

	// forward Add <-> rollback Remove, and vice versa
	assert.Equal(t, tree.OpAdd, fops["vlan 20"])
	assert.Equal(t, tree.OpRemove, bops["no vlan 20"])
	assert.Equal(t, tree.OpRemove, fops["no vlan 10"])
	assert.Equal(t, tree.OpAdd, bops["vlan 10"])
	// idempotent slot: Modify both ways, new value vs old value
	assert.Equal(t, tree.OpModify, fops["switchport access vlan 20"])
	assert.Equal(t, tree.OpModify, bops["switchport access vlan 10"])
	// toggle dedup is direction-blind: exactly one forward line each way
	assert.Equal(t, tree.OpAdd, fops["no shutdown"])
	assert.Equal(t, tree.OpAdd, bops["shutdown"])
}

func TestRollbackCommitChecksRunningGoal(t *testing.T) {
	e := confetti.New(
		remediateSchema(),
		confetti.WithPolicy(diag.Policy{Strict: true}),
	)
	// Running references an undefined VLAN and is therefore an invalid rollback goal.
	running, d1 := e.Import(
		"interface Ethernet1/1\n  switchport access vlan 99\n",
	)
	require.False(t, d1.HasErrors(), d1.String())
	intended, d2 := e.Import("")
	require.False(t, d2.HasErrors(), d2.String())

	res, d := e.Rollback(running, intended)
	assert.True(
		t,
		d.HasErrors(),
		"rolling back onto a dangling-ref goal must error",
	)
	assert.Contains(t, d.String(), `vlan "99" does not exist`)
	// the Result is still returned for inspection
	require.NotNil(t, res)
	assert.NotEqual(t, "", render.Render(res.Tree))
}

func TestRollbackNoChange(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	cfg, d := e.Import(cleanConfig)
	require.False(t, d.HasErrors(), d.String())
	res, rd := e.Rollback(cfg, cfg)
	require.False(t, rd.HasErrors(), rd.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestRollbackIsCanonicalNotByteExact(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: false})
	// lenient import drops the unknown line from the running tree, so rollback
	// restores the canonical parse of running, never its original bytes
	running, d1 := e.Import(
		"interface Ethernet1/1\n  flux-capacitor enable\n  no shutdown\n",
	)
	assert.False(t, d1.HasErrors()) // warnings only
	intended, _ := e.Import("interface Ethernet1/1\n")

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "no shutdown") // canonical content restored
	assert.NotContains(
		t,
		out,
		"flux-capacitor",
	) // dropped lines cannot come back
}

func TestBannerRemediateAndRollback(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	run, d1 := e.Import("banner motd ^\nold\n^\nvlan 10\n")
	require.False(t, d1.HasErrors(), d1.String())
	intd, d2 := e.Import("banner motd ^\nnew\n^\nvlan 10\n")
	require.False(t, d2.HasErrors(), d2.String())

	res, d := e.Remediate(run, intd)
	require.False(t, d.HasErrors())
	assert.Equal(t, "banner motd ^\nnew\n^\n", render.Render(res.Tree))

	back, bd := e.Rollback(run, intd)
	require.False(t, bd.HasErrors())
	assert.Equal(t, "banner motd ^\nold\n^\n", render.Render(back.Tree))
}

func TestRemediateAlphaPhysicalPortReset(t *testing.T) { // E7 physical
	e := alpha.Engine(diag.Policy{Strict: true})
	running, d := e.Import("interface Ethernet1/1\n  description X\n")
	require.False(t, d.HasErrors(), d.String())
	intended, d := e.Import("")
	require.False(t, d.HasErrors(), d.String())
	res, rd := e.Remediate(running, intended)
	require.False(t, rd.HasErrors(), rd.String())
	out := render.Render(res.Tree)
	assert.Equal(t, "default interface Ethernet1/1\n", out)
}

func TestRemediateAlphaLogicalInterfaceStillNegates(
	t *testing.T,
) { // E7 logical
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("interface Vlan10\n  description X\n")
	intended, _ := e.Import("")
	res, rd := e.Remediate(running, intended)
	require.False(t, rd.HasErrors(), rd.String())
	assert.Equal(t, "no interface Vlan10\n", render.Render(res.Tree))
}

func TestAlphaEthernetBindsPhysicalDef(t *testing.T) {
	// Both interface templates match "interface Ethernet1/1" at equal literal
	// specificity; declaration order (physical first) is the tie-break. Pin it.
	e := alpha.Engine(diag.Policy{Strict: true})
	cfg, d := e.Import(
		"interface Ethernet1/1\ninterface Vlan10\ninterface Ethernet1\n",
	)
	require.False(t, d.HasErrors(), d.String())
	kids := cfg.Root.Children
	require.Len(t, kids, 3)
	assert.Equal(t, schema.NegDefault, kids[0].Def.Negate.Kind)
	assert.Equal(t, schema.NegNoPrefix, kids[1].Def.Negate.Kind)
	// A bare "Ethernet1" (no /slot) is not a real port name: the ethport
	// pattern requires at least one "/N" suffix, so it falls through to the
	// generic catch-all def instead of binding the physical (default-reset) one.
	assert.Equal(t, schema.NegNoPrefix, kids[2].Def.Negate.Kind)
}

func TestRemediateAlphaProtectedBGPRefuses(t *testing.T) {
	for _, strict := range []bool{true, false} {
		e := alpha.Engine(diag.Policy{Strict: strict})
		running, _ := e.Import("router bgp 65000\n")
		intended, _ := e.Import("")
		_, d := e.Remediate(running, intended)
		assert.True(t, d.HasErrors(), "strict=%v", strict)
		assert.Contains(t, d.String(), "refusing to delete protected")
	}
}

func TestRemediateAlphaProtectedASNChangeRefuses(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("router bgp 65000\n")
	intended, _ := e.Import("router bgp 65001\n")
	_, d := e.Remediate(running, intended)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "refusing to delete protected")
}

func TestEngineMergeToggleConflict(t *testing.T) {
	// Engine.Merge must treat shutdown and no shutdown on one interface as a single conflicting slot.
	e := alpha.Engine(diag.Policy{Strict: true})
	a, da := e.Import("interface Ethernet1/1\n  shutdown\n")
	require.False(t, da.HasErrors(), da.String())
	b, db := e.Import("interface Ethernet1/1\n  no shutdown\n")
	require.False(t, db.HasErrors(), db.String())
	_, d := e.Merge(a, b)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "conflicts with")
}

func TestRollbackReAddsResetInterface(t *testing.T) { // E7 rollback direction
	// The rollback of a reset (physical port removal, "default interface")
	// converges the other way: intended (empty) -> running (has the
	// interface + description), so it must re-ADD the whole section.
	e := alpha.Engine(diag.Policy{Strict: true})
	running, dr := e.Import("interface Ethernet1/1\n  description X\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import("")
	require.False(t, di.HasErrors(), di.String())
	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out, rd := e.Render(res.Tree)
	require.False(t, rd.HasErrors(), rd.String())
	assert.Contains(t, out, "interface Ethernet1/1")
	assert.Contains(t, out, "description X")
}

func TestCompareAlphaEndToEnd(t *testing.T) {
	// Same pair as TestRemediateAlphaEndToEnd; the view regroups the
	// scheduled log by path, so both root-level changes share one hunk.
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	intended, _ := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")
	out, d := e.Compare(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"+ vlan 20\n"+
			"- vlan 10\n"+
			"  interface Ethernet1/1\n"+
			"-   switchport access vlan 10\n"+
			"+   switchport access vlan 20\n"+
			"-   shutdown\n"+
			"+   no shutdown\n",
		out)
}

func TestCompareSkipsCommitCheck(t *testing.T) {
	// Read-only view: no commit-check on either side (gofmt-vs-vet split).
	// The same pair makes Remediate error on the dangling ref.
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("")
	intended, d0 := e.Import(
		"interface Ethernet1/1\n  switchport access vlan 99\n")
	require.False(t, d0.HasErrors(), d0.String())

	_, rd := e.Remediate(running, intended)
	require.True(t, rd.HasErrors())

	out, d := e.Compare(running, intended)
	assert.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"+ interface Ethernet1/1\n+   switchport access vlan 99\n", out)
}

func TestRemediateAlphaTrunkVlanDelta(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, dr := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20,30-40\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,25,30-40\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface Ethernet1/5\n"+
			"  switchport trunk allowed vlan remove 20\n"+
			"  switchport trunk allowed vlan add 25\n",
		render.Render(res.Tree))
}

func TestRollbackAlphaTrunkVlanDeltaInverts(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20,30-40\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,25,30-40\n")

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	// The exact mirror of the remediation delta: Diff is direction-agnostic.
	assert.Equal(t,
		"interface Ethernet1/5\n"+
			"  switchport trunk allowed vlan remove 25\n"+
			"  switchport trunk allowed vlan add 20\n",
		render.Render(res.Tree))
}

func TestRemediateAlphaTrunkVlanFormattingNoChurn(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,11,12\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10-12\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestImportAlphaTrunkVlanBadItem(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	_, d := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,5000\n")
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "5000")
}

func TestCompareAlphaTrunkVlanModifyPair(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20,30-40\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,25,30-40\n")

	view, d := e.Compare(running, intended)
	require.False(t, d.HasErrors(), d.String())
	// The change log stays a state view (whole old/new lines) while the
	// remediation artifact is incremental.
	assert.Contains(t, view, "-   switchport trunk allowed vlan 10,20,30-40\n")
	assert.Contains(t, view, "+   switchport trunk allowed vlan 10,25,30-40\n")
}

func TestRemediateAlphaVlanSpellingNoChurn(t *testing.T) {
	// Equivalent section and membership spellings must fold to identical trees.
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("vlan 7\nvlan 8\nvlan 9\n")
	intended, _ := e.Import("vlan 7-9\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestRemediateAlphaVlanMembershipAddRemove(t *testing.T) {
	// Membership-declared vlans diff as ordinary canonical instances:
	// per-instance adds and header negations, not list deltas.
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("vlan 7,20\n")
	intended, _ := e.Import("vlan 7,9-10\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"vlan 9\nvlan 10\nno vlan 20\n",
		render.Render(res.Tree))
}

func TestRollbackAlphaVlanMembershipInverts(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("vlan 7,20\n")
	intended, _ := e.Import("vlan 7,9-10\n")

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	// Removals emit in descending order (creates ascend, removes descend).
	assert.Equal(t,
		"vlan 20\nno vlan 10\nno vlan 9\n",
		render.Render(res.Tree))
}

func TestEngineMergeVlanMembershipUnion(t *testing.T) {
	// Merge canonical membership instances by key, deduplicate overlap, and retain children.
	e := alpha.Engine(diag.Policy{Strict: true})
	p1, d1 := e.Import("vlan 1-2\n")
	require.False(t, d1.HasErrors(), d1.String())
	p2, d2 := e.Import("vlan 2,3\nvlan 3\n  name STORAGE\n")
	require.False(t, d2.HasErrors(), d2.String())

	merged, d := e.Merge(p1, p2)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(merged)
	assert.Equal(t, "vlan 1\nvlan 2\nvlan 3\n  name STORAGE\n", out)
}

func TestCompareAlphaVlanMembership(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	running, _ := e.Import("vlan 7\n")
	intended, _ := e.Import("vlan 7,9-10\n")

	out, d := e.Compare(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "+ vlan 9\n+ vlan 10\n", out)
}

func TestRemediateAlphaMalformedMembershipDegrades(t *testing.T) {
	// Preserve an unexpanded membership line with an Import Error and pair it by full text.
	e := alpha.Engine(diag.Policy{Strict: false})
	running, dr := e.Import("vlan 9-5\n")
	assert.True(t, dr.HasErrors())
	intended, di := e.Import("vlan 5\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan 5\nno vlan 9-5\n", render.Render(res.Tree))
}

func TestRemediateAlphaTrunkKeywordSpellingNoChurn(t *testing.T) {
	// "all" and the explicit full range are the same set: not drift.
	e := alpha.Engine(diag.Policy{Strict: true})
	running, dr := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan all\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 1-4094\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
}

func TestRemediateAlphaTrunkKeywordDeltas(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})

	// Explicit list -> none: remove exactly the running items.
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan none\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan remove 10,20\n",
		render.Render(res.Tree))

	// all -> except 10: remove exactly vlan 10.
	running2, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan all\n")
	intended2, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan except 10\n")
	res2, d2 := e.Remediate(running2, intended2)
	require.False(t, d2.HasErrors(), d2.String())
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan remove 10\n",
		render.Render(res2.Tree))
}

func TestAlphaTrunkContinuationEndToEnd(t *testing.T) {
	// The add-form is accepted as config input and folds into the base slot;
	// the folded spelling is not drift against the pre-joined one.
	e := alpha.Engine(diag.Policy{Strict: true})
	cfg, d := e.Import("interface Ethernet1/5\n" +
		"  switchport trunk allowed vlan 10\n" +
		"  switchport trunk allowed vlan add 20-22\n")
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20-22\n",
		out)

	joined, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20-22\n")
	res, rd := e.Remediate(cfg, joined)
	require.False(t, rd.HasErrors(), rd.String())
	assert.True(t, res.Empty())
}

func TestEngineMergeTrunkKeywordUnion(t *testing.T) {
	e := alpha.Engine(diag.Policy{Strict: true})
	p1, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan none\n")
	p2, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10\n")
	merged, d := e.Merge(p1, p2)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(merged)
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10\n", out)

	p3, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan all\n")
	merged2, d2 := e.Merge(p3, p2)
	require.False(t, d2.HasErrors(), d2.String())
	out2, _ := e.Render(merged2)
	// Union with the whole domain canonicalizes back to the keyword.
	assert.Equal(t,
		"interface Ethernet1/5\n  switchport trunk allowed vlan all\n", out2)
}

// refListSchema declares interface references before VLAN definitions so per-item edges must correct delta order.
func refListSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).
		List("vlans", "vlan").
		ListDelta("allowed vlan add {{ vlans }}",
			"allowed vlan remove {{ vlans }}").
		ListKeywords("none", "all", "except", "1-4094").
		Ref("vlans", "vlan.id")
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	return s
}

func TestCommitCheckPerItemListRef(t *testing.T) {
	e := confetti.New(refListSchema(),
		confetti.WithPolicy(diag.Policy{Strict: true}))

	cfg, d := e.Import("interface Ethernet1/1\n  allowed vlan 10,99\nvlan 10\n")
	require.False(t, d.HasErrors(), d.String())
	cc := e.CommitCheck(cfg)
	assert.True(t, cc.HasErrors())
	assert.Contains(t, cc.String(), `vlan "99" does not exist`)
	assert.NotContains(t, cc.String(), `"10"`)

	// Keyword spellings name no items ("all"), except names its exceptions.
	cfg2, _ := e.Import("interface Ethernet1/1\n  allowed vlan all\n")
	assert.False(t, e.CommitCheck(cfg2).HasErrors())
	cfg3, _ := e.Import("interface Ethernet1/1\n  allowed vlan except 42\n")
	cc3 := e.CommitCheck(cfg3)
	assert.True(t, cc3.HasErrors())
	assert.Contains(t, cc3.String(), `vlan "42" does not exist`)
}

func TestRemediatePerItemRefOrdersDelta(t *testing.T) {
	e := confetti.New(refListSchema(),
		confetti.WithPolicy(diag.Policy{Strict: true}))

	// Add side: the delta referencing vlan 30 must wait for its creation,
	// against declaration rank.
	running, _ := e.Import(
		"interface Ethernet1/1\n  allowed vlan 10\nvlan 10\n",
	)
	intended, _ := e.Import(
		"interface Ethernet1/1\n  allowed vlan 10,30\nvlan 10\nvlan 30\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"vlan 30\ninterface Ethernet1/1\n  allowed vlan add 30\n",
		render.Render(res.Tree))

	// Remove side: the delta must release vlan 30 before its definition goes.
	res2, d2 := e.Remediate(intended, running)
	require.False(t, d2.HasErrors(), d2.String())
	assert.Equal(t,
		"interface Ethernet1/1\n  allowed vlan remove 30\nno vlan 30\n",
		render.Render(res2.Tree))
}

func TestEngineMergeEmptyExceptUnionKeepsSpelling(
	t *testing.T,
) {
	// Keep the existing spelling when two Except forms resolve to an empty set without a None spelling.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).
		List("vlans", "vlan").
		ListKeywords("", "all", "except", "1-6")
	e := confetti.New(s, confetti.WithPolicy(diag.Policy{Strict: true}))

	p1, d1 := e.Import("interface Ethernet1/1\n  allowed vlan except 1-6\n")
	require.False(t, d1.HasErrors(), d1.String())
	p2, d2 := e.Import("interface Ethernet1/1\n  allowed vlan except 1-2,3-6\n")
	require.False(t, d2.HasErrors(), d2.String())

	merged, d := e.Merge(p1, p2)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(merged)
	assert.Equal(t,
		"interface Ethernet1/1\n  allowed vlan except 1-6\n", out)
}
