package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/value"
)

// memberSchema declares the classic dual-form vlan shape: a canonical keyed section plus a
// membership range line. The canonical definition is declared first so the
// specificity tie on "vlan " breaks toward it for single-id lines.
func memberSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	vlan := s.Node("vlan {{ id:vlan }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	vlan.Child("name {{ text:word }}").Card(schema.ZeroToOne).MarkIdempotent()
	s.Node("vlan {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan")
	return s
}

func foldConfig(t *testing.T, s *schema.Schema, in string) *schema.Config {
	t.Helper()
	d := diag.New()
	cfg := Parse(s, in, Reject, d)
	Fold(cfg, d)
	require.False(t, d.HasErrors(), d.String())
	return cfg
}

func topTexts(cfg *schema.Config) []string {
	texts := make([]string, 0, len(cfg.Root.Children))
	for _, n := range cfg.Root.Children {
		texts = append(texts, n.Text)
	}
	return texts
}

func TestFoldMembersSynthesizesBareInstances(t *testing.T) {
	cfg := foldConfig(t, memberSchema(), "vlan 1,7-9\n")
	assert.Equal(t, []string{"vlan 1", "vlan 7", "vlan 8", "vlan 9"},
		topTexts(cfg))
	for _, n := range cfg.Root.Children {
		require.NotNil(t, n.Def)
		assert.Equal(t, "vlan", n.Def.KindName)
		assert.Equal(t, 0, len(n.Children))
	}
	assert.Equal(t, "7", cfg.Root.Children[1].Fields["id"])
}

func TestFoldMembersDedupsAgainstSectionAfterLine(t *testing.T) {
	// A real-world emission order: the compressed line first, the
	// property-bearing section after it. The section must win the slot; the
	// membership item folds away.
	in := "vlan 1,7-9,411\n" +
		"vlan 411\n" +
		"  name PAYMENTS\n"
	cfg := foldConfig(t, memberSchema(), in)
	assert.Equal(t,
		[]string{"vlan 1", "vlan 7", "vlan 8", "vlan 9", "vlan 411"},
		topTexts(cfg))
	last := cfg.Root.Children[4]
	require.Equal(t, 1, len(last.Children))
	assert.Equal(t, "name PAYMENTS", last.Children[0].Text)
}

func TestFoldMembersDedupsAgainstSectionBeforeLine(t *testing.T) {
	in := "vlan 411\n" +
		"  name PAYMENTS\n" +
		"vlan 7-8,411\n"
	cfg := foldConfig(t, memberSchema(), in)
	assert.Equal(t, []string{"vlan 411", "vlan 7", "vlan 8"}, topTexts(cfg))
	assert.Equal(t, 1, len(cfg.Root.Children[0].Children))
}

func TestFoldMembersOverlappingLines(t *testing.T) {
	// The second line's overlap dedups against the first line's synthesis;
	// an all-duplicate line vanishes without residue.
	in := "vlan 1-3\nvlan 2,4\nvlan 1,3\n"
	cfg := foldConfig(t, memberSchema(), in)
	assert.Equal(t, []string{"vlan 1", "vlan 2", "vlan 3", "vlan 4"},
		topTexts(cfg))
}

func TestFoldMembersNestedLevel(t *testing.T) {
	// Membership works at any level: siblings inside a section (the submode
	// "vlan database" shape).
	s := schema.New()
	testtypes.Fill(s.Registry)
	db := s.Node("vlan database").Card(schema.ZeroToOne)
	db.Child("vlan {{ id:vlan }} bridge 1").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	db.Child("vlan {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan")

	cfg := foldConfig(t, s, "vlan database\n  vlan 5,7\n")
	top := cfg.Root.Children
	require.Len(t, top, 1)
	kids := top[0].Children
	require.Len(t, kids, 2)
	assert.Equal(t, "vlan 5 bridge 1", kids[0].Text)
	assert.Equal(t, "vlan 7 bridge 1", kids[1].Text)
}

func TestFoldMembersExpandFailureLeavesLine(t *testing.T) {
	// Keep the malformed list for ImportCheck.
	d := diag.New()
	cfg := Parse(memberSchema(), "vlan 9-5\n", Reject, d)
	Fold(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
	assert.Equal(t, []string{"vlan 9-5"}, topTexts(cfg))
}

func TestFoldMembersNoCanonicalSiblingErrors(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan") // no "vlan" Kind def anywhere
	d := diag.New()
	cfg := Parse(s, "vlan 1-3\n", Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "no canonical \"vlan\" def")
	assert.Equal(t, []string{"vlan 1-3"}, topTexts(cfg))
}

func TestFoldMembersUnsynthesizableItemErrors(t *testing.T) {
	// Reject the complete fold when one item cannot match the canonical template.
	d := diag.New()
	cfg := Parse(memberSchema(), "vlan 5,abc\n", Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "cannot synthesize")
	assert.Equal(t, []string{"vlan 5,abc"}, topTexts(cfg))
}

func TestFoldMembersCompetingSiblingDefErrors(t *testing.T) {
	// Report an Error when an earlier equal-specificity sibling matches the rendered canonical text.
	s := schema.New()
	testtypes.Fill(s.Registry)
	require.NoError(t, s.Registry.Register(
		value.Type{Name: "alnum", Pattern: `[A-Za-z0-9]+`}))
	s.Node("vlan {{ name:alnum }}").
		Card(schema.ZeroToN).Kind("vlan-name").Key("name")
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("vlan {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan")

	d := diag.New()
	cfg := Parse(s, "vlan 7-8\n", Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not re-parse")
	assert.Equal(t, []string{"vlan 7-8"}, topTexts(cfg))
}

// contSchema declares a trunk-style slot with keyword spellings plus an
// add-form continuation line.
func contSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	trunk := iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).
		List("vlans", "vlan").
		ListKeywords("none", "all", "except", "1-4094")
	iface.Child("allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToN).
		List("vlans", "vlan").
		ListContinues(trunk)
	return s
}

func ifaceKids(t *testing.T, cfg *schema.Config) []string {
	t.Helper()
	require.Len(t, cfg.Root.Children, 1)
	kids := cfg.Root.Children[0].Children
	texts := make([]string, 0, len(kids))
	for _, k := range kids {
		texts = append(texts, k.Text)
	}
	return texts
}

func TestFoldContinuationUnionsIntoBase(t *testing.T) {
	cfg := foldConfig(t, contSchema(),
		"interface Ethernet1/1\n"+
			"  allowed vlan 10\n"+
			"  allowed vlan add 20-22\n"+
			"  allowed vlan add 40\n")
	assert.Equal(t, []string{"allowed vlan 10,20-22,40"}, ifaceKids(t, cfg))
}

func TestFoldContinuationCreatesBase(t *testing.T) {
	cfg := foldConfig(t, contSchema(),
		"interface Ethernet1/1\n  allowed vlan add 20\n")
	kids := ifaceKids(t, cfg)
	assert.Equal(t, []string{"allowed vlan 20"}, kids)
	// The synthesized base binds the base def (MatchChild rail).
	base := cfg.Root.Children[0].Children[0]
	require.NotNil(t, base.Def)
	assert.Equal(t, "allowed vlan {{ vlans:rest }}", base.Def.Template)
}

func TestFoldContinuationKeywordBases(t *testing.T) {
	// none + add = the added items; all + add = still all (canonical keyword).
	cfg := foldConfig(t, contSchema(),
		"interface Ethernet1/1\n"+
			"  allowed vlan none\n"+
			"  allowed vlan add 5\n")
	assert.Equal(t, []string{"allowed vlan 5"}, ifaceKids(t, cfg))

	cfg2 := foldConfig(t, contSchema(),
		"interface Ethernet1/1\n"+
			"  allowed vlan all\n"+
			"  allowed vlan add 5\n")
	assert.Equal(t, []string{"allowed vlan all"}, ifaceKids(t, cfg2))
}

func TestFoldContinuationMalformedLeavesLine(t *testing.T) {
	d := diag.New()
	cfg := Parse(contSchema(),
		"interface Ethernet1/1\n"+
			"  allowed vlan 10\n"+
			"  allowed vlan add 9-5\n",
		Reject, d)
	Fold(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		[]string{"allowed vlan 10", "allowed vlan add 9-5"},
		ifaceKids(t, cfg))
}

func TestFoldContinuationSelectsMatchingBase(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	base := s.Node("filter {{ dir:word }} vlans {{ vlans:word }}").
		Card(schema.ZeroToN).List("vlans", "vlan")
	s.Node("filter {{ dir:word }} vlans add {{ vlans:word }}").
		Card(schema.ZeroToN).List("vlans", "vlan").ListContinues(base)

	d := diag.New()
	cfg := Parse(s,
		"filter in vlans 10\n"+
			"filter out vlans 20\n"+
			"filter out vlans add 30\n"+
			"filter local vlans add 40\n",
		Reject, d)
	Fold(cfg, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, []string{
		"filter in vlans 10",
		"filter out vlans 20,30",
		"filter local vlans 40",
	}, topTexts(cfg))
}

func TestFoldContinuationRejectsAmbiguousBase(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	base := s.Node("filter {{ dir:word }} vlans {{ vlans:word }}").
		Card(schema.ZeroToN).List("vlans", "vlan")
	s.Node("filter vlans add {{ vlans:word }}").Card(schema.ZeroToN).
		List("vlans", "vlan").ListContinues(base)
	d := diag.New()
	cfg := Parse(s,
		"filter in vlans 10\nfilter out vlans 20\nfilter vlans add 30\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "matches multiple base slots")
	assert.Equal(t, []string{
		"filter in vlans 10",
		"filter out vlans 20",
		"filter vlans add 30",
	}, topTexts(cfg))
}

func TestFoldContinuationUnionReparseRail(t *testing.T) {
	// Require union output to match the intended definition when a more-literal sibling also matches.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	trunk := iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).List("vlans", "vlan")
	iface.Child("allowed vlan 10,20").Card(schema.ZeroToOne)
	iface.Child("allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToN).List("vlans", "vlan").ListContinues(trunk)

	d := diag.New()
	cfg := Parse(s,
		"interface Ethernet1/1\n  allowed vlan 10\n  allowed vlan add 20\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not re-parse")
	assert.Equal(t,
		[]string{"allowed vlan 10", "allowed vlan add 20"},
		ifaceKids(t, cfg))
}

func TestFoldSynthesizedNodesInheritLine(t *testing.T) {
	// A fold-synthesized node has no source line of its own; it inherits the
	// folded line's number, 1-to-N (the RealIndent rule).
	cfg := foldConfig(t, memberSchema(), "vlan 40\nvlan 1,7-9\n")
	for _, n := range cfg.Root.Children[1:] {
		assert.Equal(t, 2, n.Line, n.Text)
	}
	assert.Equal(t, 1, cfg.Root.Children[0].Line)
}

func TestFoldContinuationUnionDropsLine(t *testing.T) {
	// The union is N-to-1: the slot's value comes from several source lines,
	// so the slot degrades to positionless instead of pointing an editor at
	// a line that does not contain the reported value.
	cfg := foldConfig(t, contSchema(),
		"interface Ethernet1/1\n  allowed vlan 10\n  allowed vlan add 20\n")
	slot := cfg.Root.Children[0].Children[0]
	assert.Equal(t, "allowed vlan 10,20", slot.Text)
	assert.Equal(t, 0, slot.Line)
	// An untouched base slot (no continuation folded) keeps its line.
	cfg2 := foldConfig(t, contSchema(),
		"interface Ethernet1/1\n  allowed vlan 10\n")
	assert.Equal(t, 2, cfg2.Root.Children[0].Children[0].Line)
}

// compositeSchema mirrors the composite-key submode shape: canonical instances
// keyed (id, bridge) with a state property, plus a range membership line
// carrying bridge and state to exercise composite keys with an additional property.
func compositeSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database").Card(schema.ZeroToOne)
	vdb.Child("vlan {{ id:vlan }} bridge {{ bridge:vlan }} state {{ state:word }}").
		Card(schema.ZeroToN).
		Kind("vlan").
		Key("id", "bridge").
		MarkIdempotent()
	vdb.Child("vlan {{ id:vlan }} bridge {{ bridge:vlan }} name {{ name:word }} state {{ state:word }}").
		Card(schema.ZeroToN).
		Kind("vlan").
		Key("id", "bridge").
		MarkIdempotent()
	vdb.Child("vlan {{ ids:word }} bridge {{ bridge:vlan }} state {{ state:word }}").
		Card(schema.ZeroToN).
		List("ids", "vlan").
		Members("vlan")
	return s
}

func kidTexts(n *schema.Node) []string {
	texts := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		texts = append(texts, c.Text)
	}
	return texts
}

func TestFoldMembersCompositeKeyCarriesProperties(t *testing.T) {
	cfg := foldConfig(t, compositeSchema(),
		"vlan database\n  vlan 2-4 bridge 1 state enable\n")
	vdb := cfg.Root.Children[0]
	assert.Equal(t, []string{
		"vlan 2 bridge 1 state enable",
		"vlan 3 bridge 1 state enable",
		"vlan 4 bridge 1 state enable",
	}, kidTexts(vdb))
	first := vdb.Children[0]
	assert.Equal(t, "2", first.Fields["id"])
	assert.Equal(t, "1", first.Fields["bridge"])
	assert.Equal(t, "enable", first.Fields["state"])
}

func TestFoldMembersCompositeDedupsAgreeingInstance(t *testing.T) {
	// Deduplicate an agreeing explicit instance even when it has additional fields.
	cfg := foldConfig(t, compositeSchema(),
		"vlan database\n"+
			"  vlan 3 bridge 1 name servers state enable\n"+
			"  vlan 2-4 bridge 1 state enable\n")
	assert.Equal(t, []string{
		"vlan 3 bridge 1 name servers state enable",
		"vlan 2 bridge 1 state enable",
		"vlan 4 bridge 1 state enable",
	}, kidTexts(cfg.Root.Children[0]))
}

func TestFoldMembersCompositeConflictSplicesForImportCheck(t *testing.T) {
	// Keep conflicting states as duplicate keys for ImportCheck.
	d := diag.New()
	cfg := Parse(compositeSchema(),
		"vlan database\n"+
			"  vlan 3 bridge 1 state disable\n"+
			"  vlan 2-4 bridge 1 state enable\n",
		Reject, d)
	Fold(cfg, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t, []string{
		"vlan 3 bridge 1 state disable",
		"vlan 2 bridge 1 state enable",
		"vlan 3 bridge 1 state enable",
		"vlan 4 bridge 1 state enable",
	}, kidTexts(cfg.Root.Children[0]))
}

func TestFoldMembersBridgeScopesIdentity(t *testing.T) {
	// The same VLAN ID under another bridge is a different identity.
	cfg := foldConfig(t, compositeSchema(),
		"vlan database\n"+
			"  vlan 3 bridge 2 state enable\n"+
			"  vlan 3 bridge 1 state enable\n"+
			"  vlan 3-4 bridge 1 state enable\n")
	assert.Equal(t, []string{
		"vlan 3 bridge 2 state enable",
		"vlan 3 bridge 1 state enable",
		"vlan 4 bridge 1 state enable",
	}, kidTexts(cfg.Root.Children[0]))
}

// respellSchema declares the numbered-ACL shape: the one-line entry respells
// into a submode section + child line.
func respellSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	acl := s.Node("ip access-list standard {{ id:uint }}").
		Card(schema.ZeroToN).Kind("acl").Key("id")
	acl.Child("{{ action:word }} {{ net:rest }}").Card(schema.ZeroToN)
	s.Node("access-list {{ id:uint }} {{ action:word }} {{ net:rest }}").
		Card(schema.ZeroToN).
		RespellAs("ip access-list standard {{ id }}", "{{ action }} {{ net }}")
	return s
}

func TestFoldRespellRewritesToSubmode(t *testing.T) {
	cfg := foldConfig(t, respellSchema(),
		"access-list 10 permit 192.168.1.0 0.0.0.255\n")
	require.Equal(t, []string{"ip access-list standard 10"}, topTexts(cfg))
	sec := cfg.Root.Children[0]
	assert.Equal(t, "acl", sec.Def.KindName)
	assert.Equal(t, []string{"permit 192.168.1.0 0.0.0.255"}, kidTexts(sec))
}

func TestFoldRespellMergesSameInstance(t *testing.T) {
	// Two one-line entries for one ACL build ONE section with both children,
	// in source order; an entry duplicating an existing child dedups.
	cfg := foldConfig(t, respellSchema(),
		"access-list 10 permit 10.0.0.0 0.0.0.255\n"+
			"access-list 10 deny 10.0.1.0 0.0.0.255\n"+
			"access-list 10 permit 10.0.0.0 0.0.0.255\n")
	require.Equal(t, []string{"ip access-list standard 10"}, topTexts(cfg))
	assert.Equal(t, []string{
		"permit 10.0.0.0 0.0.0.255",
		"deny 10.0.1.0 0.0.0.255",
	}, kidTexts(cfg.Root.Children[0]))
}

func TestFoldRespellMergesIntoExplicitSection(t *testing.T) {
	// A one-line entry merges into an explicitly-written submode section
	// with the same header, regardless of order.
	cfg := foldConfig(t, respellSchema(),
		"ip access-list standard 10\n  deny 10.0.1.0 0.0.0.255\n"+
			"access-list 10 permit 10.0.0.0 0.0.0.255\n")
	require.Equal(t, []string{"ip access-list standard 10"}, topTexts(cfg))
	assert.Equal(t, []string{
		"deny 10.0.1.0 0.0.0.255",
		"permit 10.0.0.0 0.0.0.255",
	}, kidTexts(cfg.Root.Children[0]))
}

func TestFoldRespellDistinctIdsStaySeparate(t *testing.T) {
	cfg := foldConfig(t, respellSchema(),
		"access-list 10 permit 10.0.0.0 0.0.0.255\n"+
			"access-list 20 deny 10.0.1.0 0.0.0.255\n")
	assert.Equal(t, []string{
		"ip access-list standard 10",
		"ip access-list standard 20",
	}, topTexts(cfg))
}

func TestFoldRespellUnbindableHeaderErrors(t *testing.T) {
	// A header that binds no canonical def refuses the fold; the line stays
	// (degraded, not silent).
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("access-list {{ id:uint }} {{ action:word }} {{ net:rest }}").
		Card(schema.ZeroToN).
		RespellAs("ip access-list standard {{ id }}", "{{ action }} {{ net }}")
	d := diag.New()
	cfg := Parse(s, "access-list 10 permit 10.0.0.0 0.0.0.255\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not bind a canonical def")
	assert.Equal(t,
		[]string{"access-list 10 permit 10.0.0.0 0.0.0.255"}, topTexts(cfg))
}

func TestFoldRespellUnbindableChildErrors(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("ip access-list standard {{ id:uint }}").
		Card(schema.ZeroToN) // no children declared
	s.Node("access-list {{ id:uint }} {{ action:word }} {{ net:rest }}").
		Card(schema.ZeroToN).
		RespellAs("ip access-list standard {{ id }}", "{{ action }} {{ net }}")
	d := diag.New()
	cfg := Parse(s, "access-list 10 permit 10.0.0.0 0.0.0.255\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not bind under")
	assert.Equal(t,
		[]string{"access-list 10 permit 10.0.0.0 0.0.0.255"}, topTexts(cfg))
}

func TestFoldMembersListArgSharesKeyName(t *testing.T) {
	// Treat a membership list argument with the canonical key name as the supplied key, not an existing field.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("vlan {{ id:word }}").Card(schema.ZeroToN).
		List("id", "vlan").Members("vlan")
	cfg := foldConfig(t, s, "vlan 1-3\n")
	assert.Equal(t, []string{"vlan 1", "vlan 2", "vlan 3"}, topTexts(cfg))
}

func TestFoldRespellChainedRefusesLoudly(t *testing.T) {
	// Reject chained RespellAs definitions because folding does not run a second pass.
	s := schema.New()
	testtypes.Fill(s.Registry)
	acl := s.Node("canon {{ id:uint }}").Card(schema.ZeroToN)
	acl.Child("body {{ x:word }}").Card(schema.ZeroToN)
	s.Node("mid {{ id:uint }} {{ x:word }}").Card(schema.ZeroToN).
		RespellAs("canon {{ id }}", "body {{ x }}")
	s.Node("outer {{ id:uint }} {{ x:word }}").Card(schema.ZeroToN).
		RespellAs("mid {{ id }} {{ x }}")
	d := diag.New()
	cfg := Parse(s, "outer 1 aa\n", Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "does not bind a canonical def")
	assert.Equal(t, []string{"outer 1 aa"}, topTexts(cfg))
}

func TestFoldContinuationMalformedBaseWarnsAndLeavesLine(t *testing.T) {
	// Warn on the continuation; ImportCheck reports the malformed base separately.
	d := diag.New()
	cfg := Parse(contSchema(),
		"interface Ethernet1/1\n"+
			"  allowed vlan 9-5\n"+
			"  allowed vlan add 20\n",
		Reject, d)
	Fold(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "continuation left unfolded")
	assert.Equal(t,
		[]string{"allowed vlan 9-5", "allowed vlan add 20"},
		ifaceKids(t, cfg))
}

// contKeywordSchema can resolve a continuation union to an empty set without a declared spelling.
func contKeywordSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	trunk := iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).
		List("vlans", "vlan").
		ListKeywords("", "", "except", "1-4")
	iface.Child("allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToN).
		List("vlans", "vlan").
		ListKeywords("none", "", "", "").
		ListContinues(trunk)
	return s
}

func TestFoldContinuationEmptyUnionNoNoneKeywordErrors(t *testing.T) {
	// Base "except <whole domain>" resolves to the empty set; the continuation
	// adds nothing. The union is empty and the base declares no none keyword,
	// so the result has no spelling: Error, both lines kept.
	d := diag.New()
	cfg := Parse(contKeywordSchema(),
		"interface Ethernet1/1\n"+
			"  allowed vlan except 1-4\n"+
			"  allowed vlan add none\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "empty union has no spelling")
	assert.Equal(t,
		[]string{"allowed vlan except 1-4", "allowed vlan add none"},
		ifaceKids(t, cfg))
}

func TestFoldContinuationSynthesizeZeroItemsErrors(t *testing.T) {
	// No base slot at the level and the continuation resolves to zero items:
	// there is nothing to synthesize a base from (the base declares no none
	// keyword). Error, line kept.
	d := diag.New()
	cfg := Parse(contKeywordSchema(),
		"interface Ethernet1/1\n  allowed vlan add none\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "continuation resolves to no items")
	assert.Equal(t, []string{"allowed vlan add none"}, ifaceKids(t, cfg))
}

func TestFoldContinuationSynthesizeReparseRail(t *testing.T) {
	// synthesizeBase enforces the same MatchChild identity rail as the union
	// path: a competing more-literal sibling def winning the rendered base
	// text means divergent pairing downstream. Error, no splice.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	trunk := iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).List("vlans", "vlan")
	iface.Child("allowed vlan 20").Card(schema.ZeroToOne)
	iface.Child("allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToN).List("vlans", "vlan").ListContinues(trunk)

	d := diag.New()
	cfg := Parse(s,
		"interface Ethernet1/1\n  allowed vlan add 20\n",
		Reject, d)
	Fold(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "cannot synthesize base slot")
	assert.Equal(t, []string{"allowed vlan add 20"}, ifaceKids(t, cfg))
}

func TestFoldTrunkSelfUnion(t *testing.T) {
	// ListContinues(self) keeps the first instance as the slot and unions later siblings into it.
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	trunk := iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToN).List("vlans", "vlan")
	trunk.ListContinues(trunk)

	d := diag.New()
	cfg := Parse(s,
		"interface Ethernet1/1\n  allowed vlan 10\n  allowed vlan 20\n",
		Reject, d)
	Fold(cfg, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(
		t,
		[]string{"allowed vlan 10,20"},
		kidTexts(cfg.Root.Children[0]),
	)
}

func TestFoldMembersSkipsDefWithTwoUncoveredKeyComponents(t *testing.T) {
	// Skip a same-Kind definition with two uncovered key fields and select the later one-key definition.
	s := schema.New()
	testtypes.Fill(s.Registry)
	vdb := s.Node("vlan database").Card(schema.ZeroToOne)
	vdb.Child("vlan {{ id:vlan }} bridge {{ bridge:vlan }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id", "bridge")
	vdb.Child("vlan {{ id:vlan }} standalone").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	vdb.Child("vlans {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan")

	cfg := foldConfig(t, s, "vlan database\n  vlans 2-3\n")
	assert.Equal(t,
		[]string{"vlan 2 standalone", "vlan 3 standalone"},
		kidTexts(cfg.Root.Children[0]))
}

func TestFoldStepsOverUnmatchedNodes(t *testing.T) {
	// Preserve unmatched nodes at every level while folding matched siblings.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("vlan {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan")
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	trunk := iface.Child("allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).List("vlans", "vlan")
	iface.Child("allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToN).List("vlans", "vlan").ListContinues(trunk)

	d := diag.New()
	cfg := Parse(s,
		"vlan 411\n"+
			"vlan 1,411\n"+
			"interface Ethernet1/1\n"+
			"  allowed vlan 10\n"+
			"  allowed vlan add 20\n",
		Drop, d)
	top := cfg.Root.Children
	raw := schema.NewNode("mystery knob 42")
	cfg.Root.InsertChildBefore(top[1], raw)
	rawKid := schema.NewNode("mystery leaf 7")
	top[2].InsertChildBefore(top[2].Children[1], rawKid)

	Fold(cfg, d)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		[]string{
			"vlan 411", "mystery knob 42", "vlan 1",
			"interface Ethernet1/1",
		},
		topTexts(cfg))
	assert.Equal(t,
		[]string{"allowed vlan 10,20", "mystery leaf 7"},
		kidTexts(cfg.Root.Children[3]))
	assert.Nil(t, raw.Def)
	assert.Equal(t, "mystery knob 42", raw.Text)
}

func TestFoldRespellPreservesSourceOrderInExplicitSection(t *testing.T) {
	// One-line entry BEFORE the explicit section: the respelled child must
	// keep its source position (ACL evaluation order), not append last.
	cfg := foldConfig(t, respellSchema(),
		"access-list 10 deny 10.0.1.0 0.0.0.255\n"+
			"ip access-list standard 10\n"+
			"  permit 10.0.0.0 0.0.0.255\n")
	require.Equal(t, []string{"ip access-list standard 10"}, topTexts(cfg))
	assert.Equal(t, []string{
		"deny 10.0.1.0 0.0.0.255",
		"permit 10.0.0.0 0.0.0.255",
	}, kidTexts(cfg.Root.Children[0]))
}
