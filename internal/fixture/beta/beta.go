package beta

import (
	"errors"
	"fmt"
	"strings"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/valcheck"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/transform"
	"github.com/acidsailor/confetti/value"
)

// Schema builds the beta fixture with bridges, VLANs, interfaces, route-maps, and BGP.
func Schema() *schema.Schema {
	s := schema.New()
	registerTypes(s)

	// Declare bridge targets before referrers and use the documented short negation form.
	s.Node("bridge {{ id:bridge }} protocol {{ proto:word }} vlan-bridge").
		Card(schema.ZeroToN).Kind("bridge").Key("id").
		NegateAs("no bridge {{ id }}")

	// Model VLAN database as an always-present mode whose keyed named and unnamed VLAN forms share identity.
	vdb := s.Node("vlan database").Card(schema.ZeroToOne).ClearOnRemove()
	for _, tmpl := range []string{
		"vlan {{ id:vlan }} bridge {{ bridge:bridge }} state {{ state:word }}",
		"vlan {{ id:vlan }} bridge {{ bridge:bridge }} name {{ name:word }} state {{ state:word }}",
	} {
		vdb.Child(tmpl).
			Card(schema.ZeroToN).
			Kind("vlan").
			Key("id", "bridge").
			MarkIdempotent().
			Ref("bridge", "bridge.id").
			NegateAs("no vlan {{ id }} bridge {{ bridge }}")
	}
	// Declare range membership after single-VLAN forms so single IDs bind to canonical definitions.
	vdb.Child("vlan {{ ids:word }} bridge {{ bridge:bridge }} state {{ state:word }}").
		Card(schema.ZeroToN).
		List("ids", "vlan").
		Members("vlan")

	// Model route-maps as header-only reference targets.
	s.Node("route-map {{ name:word }} {{ action:word }} {{ seq:uint }}").
		Card(schema.ZeroToN).Kind("route-map").Key("name", "action", "seq")

	// Define VRF targets for interface forwarding and per-VRF BGP.
	s.Node("ip vrf {{ name:word }}").
		Card(schema.ZeroToN).Kind("vrf").Key("name")

	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	iface.Child("mtu {{ mtu:mtu }}").Card(schema.ZeroToOne).MarkIdempotent()
	iface.Child("switchport").Card(schema.ZeroToOne)
	iface.Child("bridge-group {{ bridge:bridge }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		Ref("bridge", "bridge.id")
	iface.Child("switchport mode {{ mode:word }}").
		Card(schema.ZeroToOne).MarkIdempotent()
	iface.Child("ip vrf forwarding {{ name:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		Ref("name", "vrf.name")
	// Reference the ID component of the composite VLAN key.
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		Ref("vlan", "vlan.id")
	iface.Child("ip address {{ addr:cidr4 }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	iface.Child("ip address {{ addr:cidr4 }} secondary").Card(schema.ZeroToN)
	// Spell delta removal and whole-slot negation with the same vendor remove form.
	const trunkRemove = "switchport trunk allowed vlan remove {{ vlans }}"
	// Use the add form as the canonical self-union slot because beta has no verified bare-list or keyword spelling.
	trunk := iface.Child("switchport trunk allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToOne).
		List("vlans", "vlan").
		ListDelta("switchport trunk allowed vlan add {{ vlans }}", trunkRemove).
		NegateAs(trunkRemove)
	trunk.ListContinues(trunk)
	// Use literal admin-state toggles because the platform default is unknown.
	shut := iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("no shutdown").Card(schema.ZeroToOne).Toggles(shut)

	bgp := s.Node("router bgp {{ asn:asn }}").Card(schema.ZeroToOne)
	bgp.Child("bgp router-id {{ id:ipv4 }}").
		Card(schema.ZeroToOne).
		MarkIdempotent()
	bgp.Child("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN)
	af := bgp.Child("address-family {{ afi:word }} {{ safi:word }}").
		Card(schema.ZeroToN).SectionExit("exit-address-family")
	af.Child("network {{ prefix:cidr4 }}").Card(schema.ZeroToN)
	af.Child("neighbor {{ peer:ipv4 }} activate").Card(schema.ZeroToN)
	af.Child("neighbor {{ peer:ipv4 }} route-map {{ rmap:word }} {{ dir:word }}").
		Card(schema.ZeroToN).
		Ref("rmap", "route-map.name")
	// Use the documented max-paths spelling with mode identity and a mutable count.
	af.Child("max-paths {{ mode:word }} {{ paths:uint }}").
		Card(schema.ZeroToN).Key("mode").MarkIdempotent()
	// Share generic address-family children and rely on greater literal specificity for per-VRF lines.
	bgp.Child("address-family ipv4 vrf {{ vrf:word }}").
		Card(schema.ZeroToN).SectionExit("exit-address-family").
		Ref("vrf", "vrf.name").
		Adopt(af.Children...)

	return s
}

// registerTypes declares beta-specific value types in this schema registry.
func registerTypes(s *schema.Schema) {
	for _, t := range []value.Type{
		{Name: "asn", Pattern: `\d+(?:\.\d+)?`, Check: checkASN},       // Accept 4-byte and asdot forms.
		{Name: "vlan", Pattern: `\d+`, Check: valcheck.Range(2, 4094)}, // VLAN 1 is implicit.
		{Name: "bridge", Pattern: `\d+`, Check: valcheck.Range(1, 32)},
		{Name: "mtu", Pattern: `\d+`, Check: valcheck.Range(64, 65536)},
		{Name: "cidr4", Pattern: `\S+`, Check: checkCIDR4},
		{Name: "ifname", Pattern: `\S+`},
		{Name: "ipv4", Pattern: `\S+`, Check: valcheck.IPv4},
	} {
		if err := s.Registry.Register(t); err != nil {
			panic(err) // Reject invalid fixture types during construction.
		}
	}
}

// checkASN accepts a plain 4-byte ASN or RFC 5396 asdot spelling without canonicalizing equivalent forms.
func checkASN(s string) error {
	hi, lo, isDot := strings.Cut(s, ".")
	if !isDot {
		return valcheck.Range(1, 4294967295)(s)
	}
	if err := valcheck.Range(1, 65535)(hi); err != nil {
		return fmt.Errorf("asdot high part: %w", err)
	}
	if err := valcheck.Range(0, 65535)(lo); err != nil {
		return fmt.Errorf("asdot low part: %w", err)
	}
	return nil
}

// checkCIDR4 accepts only A.B.C.D/M because beta has no dotted-mask form.
func checkCIDR4(s string) error {
	if !strings.Contains(s, "/") {
		return errors.New("expected A.B.C.D/M prefix form")
	}
	return valcheck.IPv4(s)
}

// ImportTransforms remove show-output noise and section-exit tokens before parsing.
func ImportTransforms() []transform.TextRule {
	var rules []transform.TextRule
	for _, pat := range []string{
		`^\s*!`,                                // Remove comment and separator lines.
		`^\s*(Building|Current) configuration`, // Remove show headers.
		`^\s*(exit-address-family|exit)\s*$`,   // Remove section exits.
		`^\s*end\s*$`,                          // Remove the trailing end line.
	} {
		r, err := transform.DropLines(pat)
		if err != nil {
			panic(err)
		}
		rules = append(rules, r)
	}
	return rules
}

// Engine returns a beta engine with its schema and import transforms.
func Engine(policy diag.Policy) *confetti.Engine {
	return confetti.New(
		Schema(),
		confetti.WithPolicy(policy),
		confetti.WithImportText(ImportTransforms()...),
	)
}
