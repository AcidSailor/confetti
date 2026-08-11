package alpha

import (
	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/valcheck"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/transform"
	"github.com/acidsailor/confetti/value"
)

// Schema builds the alpha fixture with interface, VLAN, VRF, route-map, BGP, and three reference checks.
func Schema() *schema.Schema {
	s := schema.New()
	registerTypes(s)

	// Define VLAN targets for switchport access references.
	vlan := s.Node("vlan {{ id:vlan }}").
		Card(schema.ZeroToN).Kind("vlan").Key("id")
	vlan.Child("name {{ text:word }}").Card(schema.ZeroToOne).MarkIdempotent()
	// Declare membership after the single-VLAN form so equal-specificity single IDs bind to canonical sections.
	s.Node("vlan {{ ids:word }}").Card(schema.ZeroToN).
		List("ids", "vlan").Members("vlan")

	// Define VRF targets for interface membership references.
	s.Node("vrf context {{ name:word }}").
		Card(schema.ZeroToN).Kind("vrf-context").Key("name")

	// Give the literal BGP feature a Kind so router BGP can require and order it.
	s.Node("feature bgp").Card(schema.ZeroToOne).Kind("feature-bgp")
	s.Node("feature {{ name:word }}").Card(schema.ZeroToN)

	// Use the full route-map tuple as identity while references target only its name.
	rmap := s.Node("route-map {{ name:word }} {{ action:word }} {{ seq:uint }}").
		Card(schema.ZeroToN).
		Kind("route-map").
		Key("name", "action", "seq")
	// Permit repeated match lines and one value for each set slot.
	rmap.Child("match tag {{ tag:uint }}").Card(schema.ZeroToN)
	rmap.Child("match ip address {{ acl:word }}").Card(schema.ZeroToN)
	rmap.Child("set local-preference {{ pref:uint }}").
		Card(schema.ZeroToOne).MarkIdempotent()
	rmap.Child("set metric {{ metric:uint }}").
		Card(schema.ZeroToOne).MarkIdempotent()

	// Preserve multiline banner bodies exactly; the fixture excludes single-line banners.
	s.Node("banner motd {{ delim:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		BlockDelim("delim").
		NegateAs("no banner motd")

	// Declare physical interfaces first so equal-specificity Ethernet names use the documented reset form.
	phys := s.Node("interface {{ name:ethport }}").Card(schema.ZeroToN).
		NegateDefault()
	iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
	iface.Child("description {{ text:rest }}").
		Card(schema.ZeroToOne).MarkIdempotent()
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }}").
		Card(schema.ZeroToOne).MarkIdempotent()
	iface.Child("ip address {{ ip:ipv4 }} {{ mask:ipv4 }} secondary").
		Card(schema.ZeroToN)
	iface.Child("switchport mode {{ mode:word }}").
		Card(schema.ZeroToOne).MarkIdempotent()
	iface.Child("switchport access vlan {{ vlan:vlan }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		Ref("vlan", "vlan.id")
	iface.Child("vrf member {{ name:word }}").
		Card(schema.ZeroToOne).MarkIdempotent().
		Ref("name", "vrf-context.name")
	shut := iface.Child("shutdown").Card(schema.ZeroToOne)
	iface.Child("no shutdown").Card(schema.ZeroToOne).Toggles(shut)
	// Accept keyword and add-form trunk lists, then fold add lines into the idempotent base slot.
	trunk := iface.Child("switchport trunk allowed vlan {{ vlans:rest }}").
		Card(schema.ZeroToOne).
		List("vlans", "vlan").
		ListDelta("switchport trunk allowed vlan add {{ vlans }}",
			"switchport trunk allowed vlan remove {{ vlans }}").
		ListKeywords("none", "all", "except", "1-4094")
	iface.Child("switchport trunk allowed vlan add {{ vlans:word }}").
		Card(schema.ZeroToN).
		List("vlans", "vlan").
		ListContinues(trunk)
	phys.Adopt(iface.Children...)

	// router bgp <asn>
	bgp := s.Node("router bgp {{ asn:asn }}").
		Card(schema.ZeroToOne).Protect().Requires("feature-bgp")
	bgp.Child("neighbor {{ peer:ipv4 }} remote-as {{ ras:asn }}").
		Card(schema.ZeroToN)
	af := bgp.Child("address-family {{ afi:word }} {{ safi:word }}").
		Card(schema.ZeroToN).SectionExit("exit-address-family")
	af.Child(
		"neighbor {{ peer:ipv4 }} route-map {{ rmap:word }} {{ dir:word }}",
	).Card(schema.ZeroToN).
		Ref("rmap", "route-map.name")
	af.Child("neighbor {{ peer:ipv4 }} activate").Card(schema.ZeroToN)

	// Share child grammar with the SAFI-less address-family form while preserving the more specific two-token match.
	bgp.Child("address-family {{ afi:word }}").
		Card(schema.ZeroToN).SectionExit("exit-address-family").
		Adopt(af.Children...)

	return s
}

// ImportTransforms remove show-output noise and section-exit tokens before parsing.
func ImportTransforms() []transform.TextRule {
	var rules []transform.TextRule
	for _, pat := range []string{
		`^\s*!`,                                // Remove comment lines.
		`^\s*(Building|Current) configuration`, // Remove show headers.
		`^\s*version\b`,                        // Remove the version line.
		`^\s*(exit-address-family|exit)\s*$`,   // Remove section exits.
	} {
		r, err := transform.DropLines(pat)
		if err != nil {
			panic(err)
		}
		rules = append(rules, r)
	}
	return rules
}

// Engine returns an alpha engine with its schema and import transforms.
func Engine(policy diag.Policy) *confetti.Engine {
	return confetti.New(
		Schema(),
		confetti.WithPolicy(policy),
		confetti.WithImportText(ImportTransforms()...),
	)
}

// registerTypes declares alpha types and gives Ethernet names a specific physical-interface match.
func registerTypes(s *schema.Schema) {
	for _, t := range []value.Type{
		{Name: "ifname", Pattern: `\S+`},
		{Name: "ipv4", Pattern: `\S+`, Check: valcheck.IPv4},
		{Name: "vlan", Pattern: `\d+`, Check: valcheck.Range(1, 4094)},
		{Name: "asn", Pattern: `\d+`, Check: valcheck.Range(1, 65535)},
		{Name: "ethport", Pattern: `Ethernet\d+(?:/\d+)+`},
	} {
		if err := s.Registry.Register(t); err != nil {
			panic(err) // Reject invalid fixture types during construction.
		}
	}
}
