package confetti_test

import (
	"fmt"

	confetti "github.com/acidsailor/confetti"
	"github.com/acidsailor/confetti/schema"
)

// switchSchema defines VLANs and access ports that reference them.
func switchSchema() *schema.Schema {
	s := schema.New()

	vlan := s.Node("vlan {{ id:uint }}").
		Card(schema.ZeroToN).
		Kind("vlan").
		Key("id")
	vlan.Child("name {{ text:word }}").Card(schema.ZeroToOne).MarkIdempotent()

	iface := s.Node("interface {{ name:word }}").
		Card(schema.ZeroToN).
		Kind("interface").
		Key("name")
	iface.Child("switchport access vlan {{ vlan:uint }}").
		Card(schema.ZeroToOne).MarkIdempotent().Ref("vlan", "vlan.id")

	return s
}

// Example imports two configurations and renders the remediation commands.
func Example() {
	e := confetti.New(switchSchema())

	running, _ := e.Import(
		"vlan 10\n" +
			"  name USERS\n" +
			"vlan 30\n" +
			"  name LEGACY\n" +
			"interface Ethernet1/1\n" +
			"  switchport access vlan 10\n")

	intended, _ := e.Import(
		"vlan 10\n" +
			"  name STAFF\n" +
			"vlan 20\n" +
			"  name GUESTS\n" +
			"interface Ethernet1/1\n" +
			"  switchport access vlan 20\n")

	res, d := e.Remediate(running, intended)
	if d.HasErrors() {
		fmt.Print(d.String())
		return
	}

	artifact, _ := e.Render(res.Tree)
	fmt.Print(artifact)
	// Output:
	// vlan 10
	//   name STAFF
	// vlan 20
	//   name GUESTS
	// interface Ethernet1/1
	//   switchport access vlan 20
	// no vlan 30
}

// ExampleEngine_CommitCheck reports a reference to an undefined VLAN.
func ExampleEngine_CommitCheck() {
	e := confetti.New(switchSchema())

	cfg, _ := e.Import(
		"interface Ethernet1/1\n" +
			"  switchport access vlan 999\n")

	d := e.CommitCheck(cfg)
	fmt.Print(d.String())
	// Output:
	// 2: error: interface Ethernet1/1 / switchport access vlan 999: vlan "999" does not exist
}
