package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/testtypes"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
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

func TestPhaseABadValue(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(s, "vlan 9999\n", diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "out of range")
}

func TestPhaseABadIP(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  ip address 10.0.0.300 255.255.255.0\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
}

func TestPhaseAGoodValuesClean(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"vlan 10\ninterface Ethernet1/1\n"+
			"  ip address 10.0.0.1 255.255.255.0\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestPhaseADuplicateSingle(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  description A\n  description B\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "only one allowed")
}

func TestPhaseADuplicateKey(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(s, "vlan 10\nvlan 10\n", diag.Policy{Strict: true}, d)
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "duplicate")
}

func TestPhaseATwoInvalidArgsDeterministicOrder(t *testing.T) {
	// Two invalid args on one line emit in sorted arg order (ip before mask),
	// not map iteration order.
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(
		s,
		"interface Ethernet1/1\n  ip address 10.0.0.300 255.255.255.300\n",
		diag.Policy{Strict: true},
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	PhaseA(cfg, d)
	require.Len(t, d.Items, 2)
	assert.Contains(t, d.Items[0].Message, `invalid ip "10.0.0.300"`)
	assert.Contains(t, d.Items[1].Message, `invalid mask "255.255.255.300"`)
}

func oneSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry)
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("mtu {{ n:uint }}").Card(schema.One) // exactly one required
	return s
}

func TestPhaseARequiredOneMissing(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		oneSchema(),
		"interface Ethernet1/1\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "missing required")
}

func TestPhaseARequiredOnePresent(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		oneSchema(),
		"interface Ethernet1/1\n  mtu 1500\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestPhaseAOneDuplicate(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		oneSchema(),
		"interface Ethernet1/1\n  mtu 1500\n  mtu 9000\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "only one allowed")
}

func TestDuplicateKeyAcrossSiblingDefs(t *testing.T) {
	// Two templates sharing Kind+Key are one key space: the same key via
	// the named and name-less forms is a duplicate.
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("vlan {{ id:vlan }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	s.Node("vlan {{ id:vlan }} name {{ name:word }} state {{ state:word }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	d := diag.New()
	cfg := parse.Parse(s,
		"vlan 10 state enable\nvlan 10 name FOO state disable\n",
		diag.Policy{Strict: true}, d)
	PhaseA(cfg, d)
	assert.True(t, d.HasErrors())
	assert.Contains(t, d.String(), "duplicate key")
}

func TestPhaseARequiredOneMissingAtRoot(t *testing.T) {
	s := schema.New()
	testtypes.Fill(s.Registry)
	s.Node("hostname {{ h:word }}").Card(schema.One)
	d := diag.New()
	cfg := parse.Parse(s, "", diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	PhaseA(cfg, d)
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

func TestPhaseAToggleGroupViolation(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		toggleSchema(),
		"interface Ethernet1/1\n  duplex full\n  duplex half\n",
		diag.Policy{Strict: true},
		d,
	)
	require.False(t, d.HasErrors(), d.String())
	PhaseA(cfg, d)
	require.True(t, d.HasErrors())
	assert.Contains(
		t,
		d.String(),
		`mutually exclusive with "duplex full" (line 2)`,
	)
	assert.Equal(t, 3, d.Items[0].Line, "points at the second member")
}

func TestPhaseAToggleGroupSingleMemberClean(t *testing.T) {
	d := diag.New()
	cfg := parse.Parse(
		toggleSchema(),
		"interface Ethernet1/1\n  duplex full\n",
		diag.Policy{Strict: true},
		d,
	)
	PhaseA(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestValidateSkipsNilDefNodes(t *testing.T) {
	// Hand-built trees (and Merge output) can carry nodes no schema def ever
	// matched; both phases must skip them without crashing or reporting.
	s := miniSchema()
	cfg := tree.NewConfig(s)
	cfg.Root.AddChild(tree.NewNode("bogus unmatched line"))
	d := diag.New()
	PhaseA(cfg, d)
	CommitCheck(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func listSchema() *schema.Schema {
	s := schema.New()
	testtypes.Fill(s.Registry) // provides vlan (1..4094 range check)
	s.Node("allowed vlan {{ vlans:word }}").List("vlans", "vlan")
	return s
}

func TestPhaseAListItemChecks(t *testing.T) {
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
			cfg := parse.Parse(s, tt.cfg, diag.Policy{Strict: true}, d)
			require.False(t, d.HasErrors(), d.String())
			PhaseA(cfg, d)
			assert.True(t, d.HasErrors())
			assert.Contains(t, d.String(), tt.wantMsg)
		})
	}
}

func TestPhaseAListGoodValuesClean(t *testing.T) {
	s := listSchema()
	d := diag.New()
	cfg := parse.Parse(s, "allowed vlan 10,20-22,4094\n",
		diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	PhaseA(cfg, d)
	assert.False(t, d.HasErrors(), d.String())
}

func TestPhaseADiagnosticsCarryLines(t *testing.T) {
	s := miniSchema()
	d := diag.New()
	cfg := parse.Parse(s,
		"interface Ethernet1/1\nvlan 10\nvlan 9999\nvlan 10\n",
		diag.Policy{Strict: true}, d)
	require.False(t, d.HasErrors(), d.String())
	PhaseA(cfg, d)
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
