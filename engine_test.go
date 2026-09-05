package confetti_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/fixture/alpha"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/merge"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/transform"
)

func engineSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("shutdown").Card(schema.ZeroToOne)
	return s
}

func TestEngineImportRenderRoundTrip(t *testing.T) {
	e := confetti.New(
		engineSchema(),
		confetti.WithUnknown(parse.Reject),
	)
	cfg, d := e.Import("interface Ethernet1/1\n  shutdown\n")
	require.False(t, d.HasErrors(), d.String())
	out, rd := e.Render(cfg)
	require.False(t, rd.HasErrors(), rd.String())
	assert.Equal(t, "interface Ethernet1/1\n  shutdown\n", out)
}

func TestEngineCommitCheckExcludeTag(t *testing.T) {
	e := confetti.New(
		l2l3Schema(),
		confetti.WithUnknown(parse.Reject),
	)
	cfg, d := e.Import("interface Ethernet1/1\n  switchport\n" +
		"  switchport access vlan 20\n  ip address 10.0.0.1/24\n")
	require.False(t, d.HasErrors(), d.String())
	cd := e.CommitCheck(cfg)
	require.True(t, cd.HasErrors())
	assert.Contains(t, cd.String(), `via label "l2"`)
}

func TestEngineImportTextTransformStripsComments(t *testing.T) {
	drop, err := transform.DropLines(`^!`)
	require.NoError(t, err)
	e := confetti.New(
		engineSchema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithImportText(drop),
	)
	cfg, d := e.Import("!banner\ninterface Ethernet1/1\n  shutdown\n")
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Equal(t, "interface Ethernet1/1\n  shutdown\n", out)
}

type fakeTreeTransform struct {
	from, to string
	ran      *bool
}

func (f fakeTreeTransform) Apply(cfg *schema.Config) {
	*f.ran = true
	schema.Walk(cfg, func(n *schema.Node) {
		if n.Text == f.from {
			n.Text = f.to
		}
	})
}

func TestEngineImportTreeTransformRuns(t *testing.T) {
	ran := false
	e := confetti.New(
		engineSchema(),
		confetti.WithUnknown(parse.Drop),
		confetti.WithImportTree(
			fakeTreeTransform{from: "shutdown", to: "shutdown", ran: &ran},
		),
	)
	_, d := e.Import("interface Ethernet1/1\n  shutdown\n")
	require.False(t, d.HasErrors(), d.String())
	assert.True(t, ran, "import tree-transform should run during Import")
}

func TestEngineExportTextTransformRuns(t *testing.T) {
	tag, err := transform.PerLineSub(`shutdown`, "shutdown ! disabled")
	require.NoError(t, err)
	e := confetti.New(
		engineSchema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithExportText(tag),
	)
	cfg, d := e.Import("interface Ethernet1/1\n  shutdown\n")
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Contains(t, out, "shutdown ! disabled")
}

// addNodeTransform appends an unmatched leaf because matched nodes render from their definitions.
type addNodeTransform struct {
	text string
	ran  *bool
}

func (a addNodeTransform) Apply(cfg *schema.Config) {
	*a.ran = true
	cfg.Root.Children[0].AddChild(schema.NewNode(a.text))
}

func TestEngineExportTreeTransformRuns(t *testing.T) {
	// Export tree transforms run before render and text transforms.
	ran := false
	tag, err := transform.PerLineSub(`link down`, "link down ! tagged")
	require.NoError(t, err)
	e := confetti.New(
		engineSchema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithExportTree(addNodeTransform{text: "link down", ran: &ran}),
		confetti.WithExportText(tag),
	)
	cfg, d := e.Import("interface Ethernet1/1\n  shutdown\n")
	require.False(t, d.HasErrors(), d.String())
	out, rd := e.Render(cfg)
	require.False(t, rd.HasErrors(), rd.String())
	assert.True(t, ran, "export tree-transform should run during Render")
	assert.Contains(t, out, "  link down ! tagged")
}

func TestEngineMerge(t *testing.T) {
	e := alpha.Engine()
	a, _ := e.Import("vlan 10\n")
	b, _ := e.Import("interface Ethernet1/1\n  switchport access vlan 10\n")
	merged, d := e.Merge(merge.Options{}, a, b)
	require.False(t, d.HasErrors())
	require.False(t, e.CommitCheck(merged).HasErrors())
	out, _ := e.Render(merged)
	assert.Equal(
		t,
		"vlan 10\ninterface Ethernet1/1\n  switchport access vlan 10\n",
		out,
	)
}

func TestEngineRemediatePassesCycleToOrdering(t *testing.T) {
	s := schema.New()
	s.Node("alpha {{ v:word }}").Card(schema.ZeroToN)
	s.Node("beta {{ v:word }}").Card(schema.ZeroToN)
	s.OrderHook(func(g *graph.Graph) {
		g.AddEdge(0, 1)
		g.AddEdge(1, 0) // forced cycle
	})
	e := confetti.New(s)
	run, d := e.Import("")
	require.False(t, d.HasErrors())
	want, d2 := e.Import("alpha one\nbeta two\n")
	require.False(t, d2.HasErrors())
	res, rd := e.Remediate(run, want)
	assert.True(t, rd.HasErrors())
	assert.True(t, res.Empty())

	eb := confetti.New(s, confetti.WithCycle(remediate.Break))
	runB, _ := eb.Import("")
	wantB, _ := eb.Import("alpha one\nbeta two\n")
	resB, rdB := eb.Remediate(runB, wantB)
	assert.False(t, rdB.HasErrors(), rdB.String())
	assert.Contains(t, rdB.String(), "ordering cycle")
	assert.False(t, resB.Empty())
}

func TestEngineExportTextTransformSkipsBlocks(t *testing.T) {
	// Export rules must not alter raw block bodies or terminators.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().BlockDelim("delim")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	sub, err := transform.PerLineSub(`secret`, "REDACTED")
	require.NoError(t, err)
	e := confetti.New(s,
		confetti.WithUnknown(parse.Reject),
		confetti.WithExportText(sub),
	)
	cfg, d := e.Import(
		"banner motd ^\nsecret body line\n^\ninterface Ethernet1/1\n  description secret\n",
	)
	require.False(t, d.HasErrors(), d.String())
	out, _ := e.Render(cfg)
	assert.Contains(t, out, "secret body line")
	assert.Contains(t, out, "description REDACTED")
}

func TestEngineExportTextTransformReachesRemediationArtifact(t *testing.T) {
	// Engine.Render applies export rules to remediation leaves without definitions.
	sub, err := transform.PerLineSub(`no `, "undo ")
	require.NoError(t, err)
	e := confetti.New(alpha.Schema(),
		confetti.WithUnknown(parse.Reject),
		confetti.WithExportText(sub),
	)
	running, d := e.Import("interface Ethernet1/1\n  description X\n")
	require.False(t, d.HasErrors(), d.String())
	intended, d2 := e.Import("interface Ethernet1/1\n")
	require.False(t, d2.HasErrors(), d2.String())
	res, rd := e.Remediate(running, intended)
	require.False(t, rd.HasErrors(), rd.String())
	out, od := e.Render(res.Tree)
	require.False(t, od.HasErrors(), od.String())
	assert.Contains(t, out, "undo description X")
	assert.NotContains(t, out, "no description X")
}

func TestImportTextTransformNestedFalseOpenerStaysUnprotected(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().BlockDelim("delim")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	drop, err := transform.DropLines(`^\s*!`)
	require.NoError(t, err)
	e := confetti.New(s,
		confetti.WithUnknown(parse.Drop),
		confetti.WithImportText(drop),
	)
	cfg, d := e.Import(
		"interface Ethernet1/1\n  banner motd ^\n!noise\ninterface Ethernet1/2\n",
	)
	assert.NotContains(t, d.String(), "!noise", d.String())
	// The nested line is an unknown command.
	assert.Contains(t, d.String(), `"banner motd ^"`)
	require.Len(t, cfg.Root.Children, 2)
	assert.Equal(t, "interface Ethernet1/2", cfg.Root.Children[1].Text)
}

func TestImportNoiseIndentCannotStrandBlockOpener(t *testing.T) {
	// Combine raw and transformed span detection when shallower noise hides a block opener before rules run.
	e := alpha.Engine()
	cfg, d := e.Import("!\n banner motd !\nhello\n!\nvlan 10\nvlan 20\n")
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, cfg.Root.Children, 3)
	assert.Equal(t, []string{"hello"}, cfg.Root.Children[0].Block)
	assert.Equal(t, "vlan 20", cfg.Root.Children[2].Text)
}

func TestImportNoiseBeforeTabIndentedOpenerKeepsBody(t *testing.T) {
	e := alpha.Engine()
	cfg, d := e.Import(
		"!\n\tbanner motd ^\n!!! Authorized !!!\nexit\n^\nvlan 10\n",
	)
	require.False(t, d.HasErrors(), d.String())
	require.Len(t, cfg.Root.Children, 2)
	assert.Equal(
		t,
		[]string{"!!! Authorized !!!", "exit"},
		cfg.Root.Children[0].Block,
	)
	assert.Equal(t, "vlan 10", cfg.Root.Children[1].Text)
}

func TestImportDiagnosticLineSurvivesDropLines(t *testing.T) {
	e := alpha.Engine(confetti.WithUnknown(parse.Drop))
	_, d := e.Import("!header\n!comment\nbogus command here\n")
	require.False(t, d.HasErrors())
	var hit bool
	for _, it := range d.Items {
		if strings.Contains(it.Message, "unsupported command dropped") {
			assert.Equal(t, 3, it.Line)
			hit = true
		}
	}
	assert.True(t, hit, d.String())
}

// l2l3Schema prevents switched and routed lines from sharing an interface.
func l2l3Schema() *schema.Schema {
	s := schema.New()
	iface := s.Node("interface {{ name:word }}").
		Card(schema.ZeroToN).Key("name")
	iface.Child("switchport").Card(schema.ZeroToOne).
		Tag("l2").ExcludeTag("l3")
	iface.Child("switchport access vlan {{ vlan:uint }}").
		Card(schema.ZeroToOne).Tag("l2").ExcludeTag("l3")
	iface.Child("ip address {{ addr:word }}").
		Card(schema.ZeroToOne).Tag("l3").ExcludeTag("l2")
	return s
}

func TestEngineRemediateClearsOldModeFirst(t *testing.T) {
	e := confetti.New(
		l2l3Schema(),
		confetti.WithUnknown(parse.Reject),
	)
	running, d := e.Import("interface Ethernet1/1\n  switchport\n" +
		"  switchport access vlan 20\n")
	require.False(t, d.HasErrors(), d.String())
	intended, id := e.Import(
		"interface Ethernet1/1\n  ip address 10.0.0.1/24\n",
	)
	require.False(t, id.HasErrors(), id.String())

	res, rd := e.Remediate(running, intended)
	require.False(t, rd.HasErrors(), rd.String())
	out, od := e.Render(res.Tree)
	require.False(t, od.HasErrors(), od.String())
	assert.Equal(t, "interface Ethernet1/1\n  no switchport access vlan 20\n"+
		"  no switchport\n  ip address 10.0.0.1/24\n", out)
}

const baselineReferrer = "interface Ethernet1/1\n  switchport access vlan 1\n"

func TestWithBaselineResolvesRefOnEveryCommitPath(t *testing.T) {
	e := confetti.New(remediateSchema(), confetti.WithBaseline("vlan 1\n"))
	referrer, d := e.Import(baselineReferrer)
	require.False(t, d.HasErrors(), d.String())
	empty, d := e.Import("")
	require.False(t, d.HasErrors(), d.String())

	assert.False(t, e.CommitCheck(referrer).HasErrors())
	_, rd := e.Remediate(empty, referrer)
	assert.False(t, rd.HasErrors(), rd.String())
	_, bd := e.Rollback(referrer, empty)
	assert.False(t, bd.HasErrors(), bd.String())

	// The baseline adds targets; it does not relax the check for other values.
	other, d := e.Import("interface Ethernet1/1\n  switchport access vlan 2\n")
	require.False(t, d.HasErrors(), d.String())
	cd := e.CommitCheck(other)
	require.True(t, cd.HasErrors())
	assert.Contains(t, cd.String(), `vlan "2" does not exist`)
}

func TestWithBaselineNeverEntersPlanOrCompare(t *testing.T) {
	e := confetti.New(remediateSchema(), confetti.WithBaseline("vlan 1\n"))
	referrer, d := e.Import(baselineReferrer)
	require.False(t, d.HasErrors(), d.String())
	empty, d := e.Import("")
	require.False(t, d.HasErrors(), d.String())

	res, rd := e.Remediate(empty, referrer)
	require.False(t, rd.HasErrors(), rd.String())
	assert.Equal(t, baselineReferrer, render.Render(res.Tree))
	back, bd := e.Rollback(empty, referrer)
	require.False(t, bd.HasErrors(), bd.String())
	assert.Equal(t, "no interface Ethernet1/1\n", render.Render(back.Tree))
	view, cd := e.Compare(empty, referrer)
	require.False(t, cd.HasErrors(), cd.String())
	assert.Equal(t,
		"+ interface Ethernet1/1\n+   switchport access vlan 1\n", view)
}

// assertBaselineOrderIndependent resolves baselineReferrer with the two options applied in either order.
func assertBaselineOrderIndependent(t *testing.T, base, other confetti.Option) {
	t.Helper()
	for name, opts := range map[string][]confetti.Option{
		"baseline first": {base, other},
		"other first":    {other, base},
	} {
		t.Run(name, func(t *testing.T) {
			e := confetti.New(remediateSchema(), opts...)
			cfg, d := e.Import(baselineReferrer)
			require.False(t, d.HasErrors(), d.String())
			assert.False(t, e.CommitCheck(cfg).HasErrors())
		})
	}
}

func TestWithBaselineAppliesImportTextInBothOrders(t *testing.T) {
	drop, err := transform.DropLines(`^!`)
	require.NoError(t, err)
	assertBaselineOrderIndependent(t,
		confetti.WithBaseline("! built-ins\nvlan 1\n"),
		confetti.WithImportText(drop))
}

func TestWithBaselineRejectsUnknownLinesEvenUnderDrop(t *testing.T) {
	assert.PanicsWithValue(t,
		"confetti: baseline does not import cleanly:\n"+
			"1: error: unknown command: \"flux-capacitor enable\"\n",
		func() {
			confetti.New(remediateSchema(),
				confetti.WithUnknown(parse.Drop),
				confetti.WithBaseline("flux-capacitor enable\n"))
		})
}

func TestWithBaselineAccumulates(t *testing.T) {
	// Unterminated fragments must not splice into one line.
	e := confetti.New(remediateSchema(),
		confetti.WithBaseline("vlan 1"), confetti.WithBaseline("vlan 2"))
	for _, id := range []string{"1", "2"} {
		cfg, d := e.Import(
			"interface Ethernet1/1\n  switchport access vlan " + id + "\n")
		require.False(t, d.HasErrors(), d.String())
		assert.False(t, e.CommitCheck(cfg).HasErrors(), id)
	}
}

func TestWithBaselineReachesValidators(t *testing.T) {
	var seen []string
	record := func(_, baseline *schema.Config, _ *diag.Diagnostics) {
		seen = nil
		schema.Walk(
			baseline,
			func(n *schema.Node) { seen = append(seen, n.Text) },
		)
	}
	e := confetti.New(remediateSchema(),
		confetti.WithBaseline("vlan 1\n"),
		confetti.WithCommitChecks(record))
	cfg, d := e.Import(baselineReferrer)
	require.False(t, d.HasErrors(), d.String())
	e.CommitCheck(cfg)
	assert.Equal(t, []string{"vlan 1"}, seen)
}

func TestWithBaselineToleratesMissingRequiredNodes(t *testing.T) {
	s := remediateSchema()
	s.Node("hostname {{ h:word }}").Card(schema.One)
	e := confetti.New(s, confetti.WithBaseline("vlan 1\n"))
	cfg, d := e.Import("hostname sw1\n" + baselineReferrer)
	require.False(t, d.HasErrors(), d.String())
	assert.False(t, e.CommitCheck(cfg).HasErrors())
}

func TestWithBaselinePanicsOnInvalidValue(t *testing.T) {
	assert.PanicsWithValue(
		t,
		"confetti: baseline does not import cleanly:\n"+
			"1: error: vlan 9999: invalid id \"9999\": out of range 1..4094\n",
		func() {
			confetti.New(remediateSchema(),
				confetti.WithBaseline("vlan 9999\n"))
		},
	)
}

// A baseline is authored platform data, so every per-level rule still applies.
func TestWithBaselinePanicsOnDuplicateKey(t *testing.T) {
	assert.PanicsWithValue(
		t,
		"confetti: baseline does not import cleanly:\n"+
			"2: error: vlan 1: duplicate key \"1\"\n",
		func() {
			confetti.New(remediateSchema(),
				confetti.WithBaseline("vlan 1\nvlan 1\n"))
		},
	)
}

func TestValidatorAlwaysReceivesANonNilBaseline(t *testing.T) {
	walked := false
	record := func(_, baseline *schema.Config, _ *diag.Diagnostics) {
		require.NotNil(t, baseline)
		schema.Walk(baseline, func(*schema.Node) {})
		walked = true
	}
	// No WithBaseline: the validator must still be able to walk the baseline.
	e := confetti.New(remediateSchema(), confetti.WithCommitChecks(record))
	cfg, d := e.Import("vlan 1\n")
	require.False(t, d.HasErrors(), d.String())
	require.NotPanics(t, func() { e.CommitCheck(cfg) })
	assert.True(t, walked)
}

func TestValidatorCannotCorruptTheEngineBaseline(t *testing.T) {
	wreck := func(_, baseline *schema.Config, _ *diag.Diagnostics) {
		baseline.Root.Children = nil
	}
	e := confetti.New(remediateSchema(),
		confetti.WithBaseline("vlan 1\n"),
		confetti.WithCommitChecks(wreck))
	running, d := e.Import("vlan 1\n")
	require.False(t, d.HasErrors(), d.String())
	intended, d := e.Import(baselineReferrer)
	require.False(t, d.HasErrors(), d.String())

	// The first call runs the validator; the baseline check must still fire.
	for range 2 {
		_, rd := e.Remediate(running, intended)
		assert.Contains(t, rd.String(), "removes device-provided")
	}
}

func TestCommitCheckReportsSchemaMismatchAndStillChecksTheTree(t *testing.T) {
	e := confetti.New(remediateSchema(), confetti.WithBaseline("vlan 1\n"))
	foreign, d := confetti.New(remediateSchema()).
		Import("interface Ethernet1/1\n  switchport access vlan 9\n")
	require.False(t, d.HasErrors(), d.String())
	cd := e.CommitCheck(foreign)
	assert.Contains(t, cd.String(), "different schemas")
	// The mismatch must not swallow the tree's own relation errors.
	assert.Contains(t, cd.String(), `vlan "9" does not exist`)
}

// renumberVlan changes a baseline key through a tree transform.
type renumberVlan struct{ from, to string }

func (r renumberVlan) Apply(cfg *schema.Config) {
	schema.Walk(cfg, func(n *schema.Node) {
		if n.Fields["id"] == r.from {
			n.Fields["id"] = r.to
			n.Text = "vlan " + r.to
		}
	})
}

func TestWithBaselineAppliesImportTreeInBothOrders(t *testing.T) {
	assertBaselineOrderIndependent(t,
		confetti.WithBaseline("vlan 4094\n"),
		confetti.WithImportTree(renumberVlan{from: "4094", to: "1"}))
}

func TestWithBaselineNeverMerges(t *testing.T) {
	e := confetti.New(remediateSchema(), confetti.WithBaseline("vlan 1\n"))
	p1, d := e.Import("vlan 2\n")
	require.False(t, d.HasErrors(), d.String())
	p2, d := e.Import(baselineReferrer)
	require.False(t, d.HasErrors(), d.String())
	merged, md := e.Merge(merge.Options{}, p1, p2)
	require.False(t, md.HasErrors(), md.String())
	assert.Equal(t, "vlan 2\n"+baselineReferrer, render.Render(merged))
}

func TestCompareReportsBaselineNegation(t *testing.T) {
	e := confetti.New(remediateSchema(), confetti.WithBaseline("vlan 1\n"))
	running, d := e.Import("vlan 1\n")
	require.False(t, d.HasErrors(), d.String())
	intended, d := e.Import("")
	require.False(t, d.HasErrors(), d.String())
	view, cd := e.Compare(running, intended)
	require.True(t, cd.HasErrors())
	assert.Contains(t, cd.String(), "removes device-provided")
	// Compare still returns its view alongside the Error.
	assert.Contains(t, view, "vlan 1")
}

func TestWithBaselineRefusesToNegateBaselineObject(t *testing.T) {
	e := confetti.New(remediateSchema(), confetti.WithBaseline("vlan 1\n"))
	running, d := e.Import("vlan 1\n")
	require.False(t, d.HasErrors(), d.String())
	intended, d := e.Import(baselineReferrer)
	require.False(t, d.HasErrors(), d.String())
	_, rd := e.Remediate(running, intended)
	require.True(t, rd.HasErrors())
	assert.Contains(
		t,
		rd.String(),
		`no vlan 1: removes device-provided vlan "1" declared by the baseline`,
	)
	_, bd := e.Rollback(intended, running)
	assert.Contains(t, bd.String(), "removes device-provided")
}
