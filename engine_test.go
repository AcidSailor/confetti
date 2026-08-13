package confetti_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/fixture/alpha"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/merge"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/transform"
	"github.com/acidsailor/confetti/tree"
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

// fakeTreeTransform marks the engine's tree-transform seam as exercised by
// renaming any node whose text equals its trigger.
type fakeTreeTransform struct {
	from, to string
	ran      *bool
}

func (f fakeTreeTransform) Apply(cfg *tree.Config) {
	*f.ran = true
	tree.Walk(cfg, func(n *tree.Node) {
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

func (a addNodeTransform) Apply(cfg *tree.Config) {
	*a.ran = true
	cfg.Root.Children[0].AddChild(tree.NewNode(a.text))
}

func TestEngineExportTreeTransformRuns(t *testing.T) {
	// The export tree-transform mutates the tree before render, and export
	// text rules see the mutated text (tree -> render -> text ordering).
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
	// The text rule matched the transform-added line, pinning the ordering.
	assert.Contains(t, out, "  link down ! tagged")
}

func TestEngineMerge(t *testing.T) {
	e := alpha.Engine()
	a, _ := e.Import("vlan 10\n")
	b, _ := e.Import("interface Ethernet1/1\n  switchport access vlan 10\n")
	merged, d := e.Merge(merge.Options{}, a, b)
	require.False(t, d.HasErrors())
	// jointly consistent: the merged artifact commit-checks green
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
	// The default aborts and emits nothing.
	e := confetti.New(s)
	run, d := e.Import("")
	require.False(t, d.HasErrors())
	want, d2 := e.Import("alpha one\nbeta two\n")
	require.False(t, d2.HasErrors())
	res, rd := e.Remediate(run, want)
	assert.True(t, rd.HasErrors())
	assert.True(t, res.Empty())

	// WithCycle(Break) drops an edge, warns, and emits the artifact.
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
	assert.Contains(t, out, "secret body line")     // block body untouched
	assert.Contains(t, out, "description REDACTED") // outside text transformed
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
	// A nested line whose text matches a root block-opener template must not
	// open a false protected span: before level-aware matching, the guard
	// disabled text rules from such a line through to EOF (no terminator ever
	// arrives), so later noise survived to the parser as misleading
	// unknown-command diagnostics.
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
	// DropLines kept firing past the pseudo-opener: the noise line was
	// blanked, so no diagnostic names it.
	assert.NotContains(t, d.String(), "!noise", d.String())
	// The stray nested line itself is ordinary unknown-command territory.
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
	// Same shape with a tab-indented opener and a body that DropLines rules
	// match ("!..." and "exit" are both noise patterns outside blocks): the
	// body must arrive byte-exact with zero diagnostics.
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
	// The bundled DropLines transform blanks instead of removing, so a
	// diagnostic after dropped comment lines carries the ORIGINAL file line.
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
