package valcheck_test

import (
	"testing"

	"github.com/acidsailor/confetti/internal/valcheck"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPv4(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"10.0.0.1", ""},
		{"10.0.0.1/24", ""},
		{"10.0.0.1/0", ""},
		{"10.0.0.1/32", ""},
		{"10.0.0.1/33", `invalid IPv4 prefix length "33"`},
		{"10.0.0.1/x", `invalid IPv4 prefix length "x"`},
		{"::1", "not an IPv4 address"},
		{"nope", "not an IPv4 address"},
	} {
		err := valcheck.IPv4(tc.in)
		if tc.want == "" {
			require.NoError(t, err, tc.in)
			continue
		}
		require.EqualError(t, err, tc.want, tc.in)
	}
}

func TestRange(t *testing.T) {
	check := valcheck.Range(1, 4094)
	assert.NoError(t, check("1"))
	assert.NoError(t, check("4094"))
	assert.EqualError(t, check("0"), "out of range 1..4094")
	assert.EqualError(t, check("4095"), "out of range 1..4094")
	assert.EqualError(t, check("x"), "not a number")

	// The 4-byte ASN bound needs the full uint64 width.
	assert.NoError(t, valcheck.Range(1, 4294967295)("4294967295"))
}
