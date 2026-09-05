package confetti_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/fixture/alpha"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/merge"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
)

const cleanConfig = "vlan 10\n" +
	"  name USERS\n" +
	"interface Ethernet1/1\n" +
	"  switchport mode access\n" +
	"  switchport access vlan 10\n" +
	"  no shutdown\n"

func TestE2ECanonicalRoundTrip(t *testing.T) {
	e := alpha.Engine()
	cfg, d := e.Import(cleanConfig)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Equal(t, cleanConfig, out)
	cfg2, _ := e.Import(out)
	out2, _ := e.Render(cfg2)
	assert.Equal(t, cleanConfig, out2)
}

func TestE2EDanglingRefThenFixed(t *testing.T) {
	e := alpha.Engine()

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

func TestE2EBrownfieldDropVsReject(t *testing.T) {
	in := "interface Ethernet1/1\n" +
		"  flux-capacitor enable\n" +
		"  no shutdown\n"

	lenient := alpha.Engine(confetti.WithUnknown(parse.Drop))
	cfg, d := lenient.Import(in)
	assert.False(t, d.HasErrors())
	assert.NotEmpty(t, d.Items)
	out, _ := lenient.Render(cfg)
	assert.NotContains(t, out, "flux-capacitor")
	assert.Contains(t, out, "no shutdown")

	strict := alpha.Engine()
	_, ds := strict.Import(in)
	assert.True(t, ds.HasErrors())
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

func TestEngineRemediateCommitChecksIntended(t *testing.T) {
	e := confetti.New(
		remediateSchema(),
		confetti.WithUnknown(parse.Reject),
	)
	running, d1 := e.Import("")
	require.False(t, d1.HasErrors())
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
	require.NotNil(t, res)
	assert.NotEqual(t, "", render.Render(res.Tree))
}

func TestEngineRemediateClean(t *testing.T) {
	e := confetti.New(
		remediateSchema(),
		confetti.WithUnknown(parse.Reject),
	)
	running, _ := e.Import("vlan 10\n")
	intended, _ := e.Import("vlan 10\nvlan 20\n")
	res, d := e.Remediate(running, intended)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, "vlan 20\n", render.Render(res.Tree))
}

// engineOpsByText indexes engine results by operation text.
func engineOpsByText(res *remediate.Result) map[string]schema.Op {
	ops := map[string]schema.Op{}
	schema.Walk(res.Tree, func(n *schema.Node) { ops[n.Text] = n.Op })
	return ops
}

func TestRollbackOpsMirrorRemediate(t *testing.T) {
	e := alpha.Engine()
	running, _ := e.Import("vlan 10\n" +
		"interface Ethernet1/1\n  switchport access vlan 10\n  shutdown\n")
	intended, _ := e.Import("vlan 20\n" +
		"interface Ethernet1/1\n  switchport access vlan 20\n  no shutdown\n")

	fwd, _ := e.Remediate(running, intended)
	back, _ := e.Rollback(running, intended)
	fops := engineOpsByText(fwd)
	bops := engineOpsByText(back)

	assert.Equal(t, schema.OpAdd, fops["vlan 20"])
	assert.Equal(t, schema.OpRemove, bops["no vlan 20"])
	assert.Equal(t, schema.OpRemove, fops["no vlan 10"])
	assert.Equal(t, schema.OpAdd, bops["vlan 10"])
	assert.Equal(t, schema.OpModify, fops["switchport access vlan 20"])
	assert.Equal(t, schema.OpModify, bops["switchport access vlan 10"])
	assert.Equal(t, schema.OpAdd, fops["no shutdown"])
	assert.Equal(t, schema.OpAdd, bops["shutdown"])
}

func TestRollbackCommitChecksRunningGoal(t *testing.T) {
	e := confetti.New(
		remediateSchema(),
		confetti.WithUnknown(parse.Reject),
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
	require.NotNil(t, res)
	assert.NotEqual(t, "", render.Render(res.Tree))
}

func TestRollbackNoChange(t *testing.T) {
	e := alpha.Engine()
	cfg, d := e.Import(cleanConfig)
	require.False(t, d.HasErrors(), d.String())
	res, rd := e.Rollback(cfg, cfg)
	require.False(t, rd.HasErrors(), rd.String())
	assert.True(t, res.Empty())
	assert.Equal(t, "", render.Render(res.Tree))
}

func TestRollbackIsCanonicalNotByteExact(t *testing.T) {
	e := alpha.Engine(confetti.WithUnknown(parse.Drop))
	running, d1 := e.Import(
		"interface Ethernet1/1\n  flux-capacitor enable\n  no shutdown\n",
	)
	assert.False(t, d1.HasErrors())
	intended, _ := e.Import("interface Ethernet1/1\n")

	res, d := e.Rollback(running, intended)
	require.False(t, d.HasErrors(), d.String())
	out := render.Render(res.Tree)
	assert.Contains(t, out, "no shutdown")
	assert.NotContains(
		t,
		out,
		"flux-capacitor",
	) // dropped lines cannot come back
}

func TestCompareSkipsCommitCheck(t *testing.T) {
	e := alpha.Engine()
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
		confetti.WithUnknown(parse.Reject))

	cfg, d := e.Import("interface Ethernet1/1\n  allowed vlan 10,99\nvlan 10\n")
	require.False(t, d.HasErrors(), d.String())
	cc := e.CommitCheck(cfg)
	assert.True(t, cc.HasErrors())
	assert.Contains(t, cc.String(), `vlan "99" does not exist`)
	assert.NotContains(t, cc.String(), `"10"`)

	// All names no explicit items; Except names its exceptions.
	cfg2, _ := e.Import("interface Ethernet1/1\n  allowed vlan all\n")
	assert.False(t, e.CommitCheck(cfg2).HasErrors())
	cfg3, _ := e.Import("interface Ethernet1/1\n  allowed vlan except 42\n")
	cc3 := e.CommitCheck(cfg3)
	assert.True(t, cc3.HasErrors())
	assert.Contains(t, cc3.String(), `vlan "42" does not exist`)
}

func TestWithCommitChecksRunsOnEveryCommitCheckingPath(t *testing.T) {
	noUsersVlan := func(cfg, _ *schema.Config, d *diag.Diagnostics) {
		schema.Walk(cfg, func(n *schema.Node) {
			// Bind to the schema definition instead of any node carrying a text field.
			if n.Parent == nil || n.Parent.Def == nil ||
				n.Parent.Def.KindName != "vlan" {
				return
			}
			if n.Fields["text"] == "USERS" {
				d.AddAt(
					n.Line,
					diag.Error,
					"%s: name USERS is reserved",
					n.Path(),
				)
			}
		})
	}
	e := confetti.New(alpha.Schema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithCommitChecks(noUsersVlan))

	bad, d := e.Import("vlan 10\n  name USERS\n")
	require.False(t, d.HasErrors(), d.String())
	good, d2 := e.Import("vlan 10\n  name GUESTS\n")
	require.False(t, d2.HasErrors(), d2.String())

	cc := e.CommitCheck(bad)
	require.True(t, cc.HasErrors())
	assert.Contains(t, cc.String(), "name USERS is reserved")
	assert.False(t, e.CommitCheck(good).HasErrors())

	// Remediate checks intended; Rollback checks running.
	res, rd := e.Remediate(good, bad)
	assert.Contains(t, rd.String(), "name USERS is reserved")
	// A failing validator reports without suppressing the operations.
	require.NotNil(t, res)
	assert.NotEmpty(t, render.Render(res.Tree))
	_, bd := e.Rollback(bad, good)
	assert.Contains(t, bd.String(), "name USERS is reserved")

	_, cd := e.Compare(good, bad)
	assert.NotContains(t, cd.String(), "name USERS is reserved")
}

func TestCommitChecksRunExactlyOncePerCommitPath(t *testing.T) {
	calls := 0
	count := func(_, _ *schema.Config, _ *diag.Diagnostics) { calls++ }
	e := confetti.New(alpha.Schema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithCommitChecks(count))

	cfg, d := e.Import(cleanConfig)
	require.False(t, d.HasErrors(), d.String())
	other, d2 := e.Import("vlan 10\n  name GUESTS\n")
	require.False(t, d2.HasErrors(), d2.String())

	tests := []struct {
		name string
		call func()
		want int
	}{
		{"CommitCheck", func() { e.CommitCheck(cfg) }, 1},
		{"Remediate", func() { e.Remediate(other, cfg) }, 1},
		{"Rollback", func() { e.Rollback(other, cfg) }, 1},
		{"Compare", func() { e.Compare(other, cfg) }, 0},
		{"Render", func() { e.Render(cfg) }, 0},
		{"Merge", func() { e.Merge(merge.Options{}, cfg, other) }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls = 0
			tt.call()
			assert.Equal(t, tt.want, calls)
		})
	}
}

func TestCommitChecksRunInRegistrationOrderAfterBuiltIn(t *testing.T) {
	mark := func(msg string) confetti.Validator {
		return func(_, _ *schema.Config, d *diag.Diagnostics) {
			d.Add(diag.Error, "%s", msg)
		}
	}
	e := confetti.New(alpha.Schema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithCommitChecks(mark("first"), mark("second")),
		confetti.WithCommitChecks(mark("third")))

	cfg, d := e.Import("interface Ethernet1/1\n  switchport access vlan 99\n")
	require.False(t, d.HasErrors(), d.String())

	cc := e.CommitCheck(cfg)
	require.Len(t, cc.Items, 4)
	assert.Contains(t, cc.Items[0].Message, `vlan "99" does not exist`)
	assert.Equal(t, "first", cc.Items[1].Message)
	assert.Equal(t, "second", cc.Items[2].Message)
	assert.Equal(t, "third", cc.Items[3].Message)
}

func TestCommitCheckKeepsDiagnosticsFromClobberingValidator(t *testing.T) {
	clobber := func(_, _ *schema.Config, d *diag.Diagnostics) { d.Items = nil }
	e := confetti.New(alpha.Schema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithCommitChecks(clobber))

	cfg, d := e.Import("interface Ethernet1/1\n  switchport access vlan 99\n")
	require.False(t, d.HasErrors(), d.String())

	cc := e.CommitCheck(cfg)
	assert.True(t, cc.HasErrors())
	assert.Contains(t, cc.String(), `vlan "99" does not exist`)
}

func TestWithCommitChecksNilFuncPanics(t *testing.T) {
	assert.PanicsWithValue(
		t,
		"confetti: WithCommitChecks with nil func",
		func() { confetti.WithCommitChecks(nil) },
	)
}

func TestRemediatePerItemRefOrdersDelta(t *testing.T) {
	e := confetti.New(refListSchema(),
		confetti.WithUnknown(parse.Reject))

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

	// The delta must release VLAN 30 before its definition is removed.
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
	e := confetti.New(s, confetti.WithUnknown(parse.Reject))

	p1, d1 := e.Import("interface Ethernet1/1\n  allowed vlan except 1-6\n")
	require.False(t, d1.HasErrors(), d1.String())
	p2, d2 := e.Import("interface Ethernet1/1\n  allowed vlan except 1-2,3-6\n")
	require.False(t, d2.HasErrors(), d2.String())

	merged, d := e.Merge(merge.Options{}, p1, p2)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(merged)
	assert.Equal(t,
		"interface Ethernet1/1\n  allowed vlan except 1-6\n", out)
}
