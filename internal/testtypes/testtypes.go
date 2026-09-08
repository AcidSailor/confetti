package testtypes

import (
	"github.com/acidsailor/confetti/internal/valcheck"
	"github.com/acidsailor/confetti/value"
)

// Fill registers test value types and panics on registration failure.
func Fill(r *value.Registry) {
	for _, t := range []value.Type{
		{Name: "ifname", Pattern: `\S+`},
		{Name: "ipv4", Pattern: `\S+`, Check: valcheck.IPv4},
		{Name: "vlan", Pattern: `\d+`, Check: valcheck.Range(1, 4094)},
		{Name: "asn", Pattern: `\d+`, Check: valcheck.Range(1, 65535)},
		{Name: "text", Pattern: `.*`}, // Allows empty text; the built-in rest type requires content.
	} {
		if err := r.Register(t); err != nil {
			panic(err)
		}
	}
}
