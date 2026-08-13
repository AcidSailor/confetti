package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/schema"
)

func miniSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").Card(schema.ZeroToOne)
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}").
		Card(schema.ZeroToOne)
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne)
	s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
	return s
}

func TestImportCheckBadValue(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(s, "vlan 9999\n", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "out of range")
}

func TestImportCheckBadIP(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  ip address 10.0.0.300 255.255.255.0\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
}

func TestImportCheckGoodValuesClean(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"vlan 10\ninterface Ethernet1/1\n"+
			"  ip address 10.0.0.1 255.255.255.0\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestImportCheckDuplicateSingle(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  description A\n  description B\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "only one allowed")
}

func TestImportCheckDuplicateKey(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(s, "vlan 10\nvlan 10\n", parse.Reject, d)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "duplicate")
}

func TestImportCheckTwoInvalidArgsDeterministicOrder(t *testing.T) {
	// Invalid arguments produce diagnostics in sorted order.
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  ip address 10.0.0.300 255.255.255.300\n",
		parse.Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	ImportCheck(cfg, d)
	require.Len(t, d.Items, 2)
	assert.Contains(t, d.Items[0].Message, `invalid ip "10.0.0.300"`)
	assert.Contains(t, d.Items[1].Message, `invalid mask "255.255.255.300"`)
}

func oneSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("mtu {{ n:uint }}").Card(schema.One)
	return s
}

func TestImportCheckRequiredOneMissing(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		oneSchema(),
		"interface Ethernet1/1\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "missing required")
}

func TestImportCheckRequiredOnePresent(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		oneSchema(),
		"interface Ethernet1/1\n  mtu 1500\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestImportCheckOneDuplicate(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		oneSchema(),
		"interface Ethernet1/1\n  mtu 1500\n  mtu 9000\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "only one allowed")
}

func TestDuplicateKeyAcrossSiblingDefs(t *testing.T) {
	// Definitions with the same Kind and Key share one key space.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("vlan {{ id:vlan }} name {{ name:word }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	d := diag.New()
	cfg := parse.Parse(s,
		"vlan 10 state enable\nvlan 10 name FOO state disable\n",
		parse.Reject, d)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "duplicate key")
}

func TestImportCheckRequiredOneMissingAtRoot(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("hostname {{ h:word }}").Card(schema.One)
	d := diag.New()
	cfg := parse.Parse(s, "", parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	ImportCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(t, d.String(), `<root>: missing required`)
}

func toggleSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	full := iface.Child("duplex full").Card(schema.ZeroToOne)
	iface.Child("duplex half").Card(schema.ZeroToOne).Toggles(full)
	return s
}

func TestImportCheckToggleGroupViolation(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		toggleSchema(),
		"interface Ethernet1/1\n  duplex full\n  duplex half\n",
		parse.Reject,
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	ImportCheck(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(
		t,
		d.String(),
		`mutually exclusive with "duplex full" (line 2)`,
	)
	assert.Equal(t, 3, d.Items[0].Line, "points at the second member")
}

func TestImportCheckToggleGroupSingleMemberClean(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		toggleSchema(),
		"interface Ethernet1/1\n  duplex full\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestValidateSkipsNilDefNodes(t *testing.T) {
	// ImportCheck and CommitCheck ignore unmatched nodes in hand-built or merged trees.
	s := miniSchema()
	cfg := schema.NewConfig(s)
	cfg.Root.AddChild(schema.NewNode("bogus unmatched line"))
	d := diag.New()
	ImportCheck(cfg, d)
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func listSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry) // provides vlan (1..4094 range check)
	s.Node("allowed vlan {{ vlans:word }}").List("vlans", "vlan")
	return s
}

func TestImportCheckListItemChecks(t *testing.T) {
	tests := []struct {
		name, cfg, wantMsg string
	}{
		{"item out of range", "allowed vlan 10,5000\n", "5000"},
		{"item fails pattern", "allowed vlan 10,abc\n", "abc"},
		{"empty element", "allowed vlan 10,,20\n", "empty element"},
		{"reversed range", "allowed vlan 30-20\n", "reversed range"},
		{"open range", "allowed vlan 10-\n", "open range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := listSchema()
			d := diag.New()
			cfg := parse.Parse(s, tt.cfg, parse.Reject, d)
			require.False(t, d.HasErrors(), d.String())
			ImportCheck(cfg, d)
			assert.True(t, d.HasErrors())
			assert.Contains(t, d.String(), tt.wantMsg)
		})
	}
}

func TestImportCheckListGoodValuesClean(t *testing.T) {
	s := listSchema()
	d := diag.New()
	cfg := parse.Parse(s, "allowed vlan 10,20-22,4094\n",
		parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	ImportCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestImportCheckDiagnosticsCarryLines(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(s,
		"interface Ethernet1/1\nvlan 10\nvlan 9999\nvlan 10\n",
		parse.Reject, d)
	require.False(t, d.HasErrors(), d.String())
	ImportCheck(cfg, d)
	require.True(t, d.HasErrors())
	byMsg := map[string]int{}
	for _, it := range d.Items {
		byMsg[it.Message] = it.Line
	}
	assert.Equal(
		t,
		3,
		byMsg[`vlan 9999: invalid id "9999": out of range 1..4094`],
	)
	assert.Equal(t, 4, byMsg[`vlan 10: duplicate key "10"`])
}

func TestImportCheckDuplicateKindSpelling(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.ZeroToOne).Kind("default-originate")
	r.Child("default-originate").
		Card(schema.ZeroToOne).Kind("default-originate")
	r.Child("send-community").Card(schema.ZeroToOne)
	r.Child("send-community extended").Card(schema.ZeroToOne)

	d := diag.New()
	cfg := parse.Parse(
		s,
		"router bgp 65000\n  default-originate\n"+
			"  default-originate route-map RM\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "spelling")

	d = diag.New()
	cfg = parse.Parse(
		s,
		"router bgp 65000\n  send-community\n  send-community extended\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestImportCheckKindSlotSatisfiesRequiredOne(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	r.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.One).Kind("default-originate")
	r.Child("default-originate").Card(schema.One).Kind("default-originate")

	for _, text := range []string{
		"router bgp 65000\n  default-originate\n",
		"router bgp 65000\n  default-originate route-map RM\n",
	} {
		d := diag.New()
		cfg := parse.Parse(s, text, parse.Reject, d)
		ImportCheck(cfg, d)
		assert.False(t, d.HasErrors(), "%q: %s", text, d.String())
	}

	d := diag.New()
	cfg := parse.Parse(s, "router bgp 65000\n", parse.Reject, d)
	ImportCheck(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "missing required")
}

func TestImportCheckKindSpellingsAtDifferentLevelsAreFine(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	v := s.Node("vrf {{ name:word }}").Card(schema.ZeroToN).Key("name")
	v.Child("default-originate route-map {{ rmap:word }}").
		Card(schema.ZeroToOne).Kind("default-originate")
	v.Child("default-originate").
		Card(schema.ZeroToOne).Kind("default-originate")

	d := diag.New()
	cfg := parse.Parse(
		s,
		"vrf RED\n  default-originate\n"+
			"vrf BLUE\n  default-originate route-map RM\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestImportCheckThreeKindSpellingsReportEachExtra(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	r := s.Node("router bgp {{ as:asn }}").Card(schema.ZeroToOne)
	for _, tmpl := range []string{
		"default-originate route-map {{ rmap:word }}",
		"default-originate always",
		"default-originate",
	} {
		r.Child(tmpl).Card(schema.ZeroToOne).Kind("default-originate")
	}
	d := diag.New()
	cfg := parse.Parse(
		s,
		"router bgp 65000\n  default-originate\n  default-originate always\n"+
			"  default-originate route-map RM\n",
		parse.Reject,
		d,
	)
	ImportCheck(cfg, d)
	assert.Equal(
		t,
		2,
		strings.Count(d.String(), "duplicate spelling"),
		d.String(),
	)
}
