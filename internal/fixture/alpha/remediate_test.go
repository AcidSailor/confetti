package alpha

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

func indexOf(haystack, needle string) int {
	return strings.Index(haystack, needle)
}

func TestRemediateAlphaEndToEnd(t *testing.T) {
	e := Engine()

	running, dr := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	require.False(t, dr.HasErrors(), dr.String())

	intended, di := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)

	assert.Equal(t,
		"vlan 20\n"+
			"interface Ethernet1/1\n"+
			"  switchport access vlan 20\n"+
			"  no shutdown\n"+
			"no vlan 10\n",
		out)
}

func TestRemediateAlphaAddressFamilyExit(t *testing.T) {
	e := Engine()
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
	// Define the route-map before its BGP referrer.
	assert.Less(
		t,
		indexOf(out, "route-map RM permit 10"),
		indexOf(out, "router bgp 65001"),
	)
}

func TestRemediateAlphaIdempotent(t *testing.T) {
	e := Engine()
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
) {
	e := Engine()
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

	assert.Contains(t, out, "no address-family ipv4 unicast")
	assert.NotContains(t, out, "exit-address-family")
}

func TestRemediateAlphaSafilessAddressFamilyExit(t *testing.T) {
	e := Engine()
	running, _ := e.Import("feature bgp\nrouter bgp 65001\n")
	intended, di := e.Import(
		"feature bgp\nrouter bgp 65001\n" +
			"  address-family ipv4\n" + // Shared grammar without a SAFI.
			"    neighbor 10.0.0.1 activate\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "address-family ipv4\n")
	assert.Contains(t, out, "exit-address-family")
}

func TestRemediateAlphaRemovedSafilessAddressFamilyNoExit(t *testing.T) {
	e := Engine()
	running, dr := e.Import(
		"feature bgp\nrouter bgp 65001\n" +
			"  address-family ipv4\n" +
			"    neighbor 10.0.0.1 activate\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, _ := e.Import("feature bgp\nrouter bgp 65001\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "no address-family ipv4")
	assert.NotContains(t, out, "exit-address-family")
}

func TestRemediateAlphaFeatureBeforeRouterBGP(t *testing.T) {
	e := Engine()
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

func TestBannerRemediateAndRollback(t *testing.T) {
	e := Engine()
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

func TestRemediateAlphaPhysicalPortReset(t *testing.T) {
	e := Engine()
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
) {
	e := Engine()
	running, _ := e.Import("interface Vlan10\n  description X\n")
	intended, _ := e.Import("")
	res, rd := e.Remediate(running, intended)
	require.False(t, rd.HasErrors(), rd.String())
	assert.Equal(t, "no interface Vlan10\n", render.Render(res.Tree))
}

func TestRemediateAlphaProtectedBGPRefuses(t *testing.T) {
	for _, cycle := range []remediate.Cycle{remediate.Abort, remediate.Break} {
		e := Engine(confetti.WithCycle(cycle))
		running, _ := e.Import("router bgp 65000\n")
		intended, _ := e.Import("")
		_, d := e.Remediate(running, intended)
		assert.True(t, d.HasErrors(), "cycle=%v", cycle)
		assert.Contains(t, d.String(), "refusing to delete protected")
	}
}

func TestRemediateAlphaProtectedASNChangeRefuses(t *testing.T) {
	e := Engine()
	running, _ := e.Import("router bgp 65000\n")
	intended, _ := e.Import("router bgp 65001\n")
	_, d := e.Remediate(running, intended)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "refusing to delete protected")
}

func TestRemediateAlphaTrunkVlanDelta(t *testing.T) {
	e := Engine()
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

func TestRemediateAlphaTrunkVlanFormattingNoChurn(t *testing.T) {
	e := Engine()
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,11,12\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10-12\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestRemediateAlphaVlanSpellingNoChurn(t *testing.T) {
	// Equivalent section and membership spellings must fold to identical trees.
	e := Engine()
	running, _ := e.Import("vlan 7\nvlan 8\nvlan 9\n")
	intended, _ := e.Import("vlan 7-9\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestRemediateAlphaVlanMembershipAddRemove(t *testing.T) {
	e := Engine()
	running, _ := e.Import("vlan 7,20\n")
	intended, _ := e.Import("vlan 7,9-10\n")

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"vlan 9\nvlan 10\nno vlan 20\n",
		render.Render(res.Tree))
}

func TestRemediateAlphaMalformedMembershipDegrades(t *testing.T) {
	// Preserve an unexpanded membership line with an Import Error and pair it by full text.
	e := Engine(confetti.WithUnknown(parse.Drop))
	running, dr := e.Import("vlan 9-5\n")
	assert.True(t, dr.HasErrors())
	intended, di := e.Import("vlan 5\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan 5\nno vlan 9-5\n", render.Render(res.Tree))
}

func TestRemediateAlphaTrunkKeywordSpellingNoChurn(t *testing.T) {
	// All and the explicit full range represent the same set.
	e := Engine()
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
	e := Engine()

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

func TestRollbackAlphaInvertsRemediate(t *testing.T) {
	e := Engine()
	running, dr := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	require.False(t, dr.HasErrors(), dr.String())
	intended, di := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")
	require.False(t, di.HasErrors(), di.String())

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"vlan 10\n"+
			"interface Ethernet1/1\n"+
			"  switchport access vlan 10\n"+
			"  shutdown\n"+
			"no vlan 20\n",
		render.Render(res.Tree))
}

func TestRollbackAlphaTrunkVlanDeltaInverts(t *testing.T) {
	e := Engine()
	running, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,20,30-40\n")
	intended, _ := e.Import(
		"interface Ethernet1/5\n  switchport trunk allowed vlan 10,25,30-40\n")

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"interface Ethernet1/5\n"+
			"  switchport trunk allowed vlan remove 25\n"+
			"  switchport trunk allowed vlan add 20\n",
		render.Render(res.Tree))
}

func TestRollbackAlphaVlanMembershipInverts(t *testing.T) {
	e := Engine()
	running, _ := e.Import("vlan 7,20\n")
	intended, _ := e.Import("vlan 7,9-10\n")

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"vlan 20\nno vlan 10\nno vlan 9\n",
		render.Render(res.Tree))
}

func TestRollbackReAddsResetInterface(t *testing.T) {
	e := Engine()
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

func TestAlphaEthernetBindsPhysicalDef(t *testing.T) {
	e := Engine()
	cfg, d := e.Import(
		"interface Ethernet1/1\ninterface Vlan10\ninterface Ethernet1\n",
	)
	require.False(t, d.HasErrors(), d.String())
	kids := cfg.Root.Children
	require.Len(t, kids, 3)
	assert.Equal(t, schema.NegDefault, kids[0].Def.Negate.Kind)
	assert.Equal(t, schema.NegNoPrefix, kids[1].Def.Negate.Kind)
	assert.Equal(t, schema.NegNoPrefix, kids[2].Def.Negate.Kind)
}
