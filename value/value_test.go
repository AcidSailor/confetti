package value

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuiltinsAreStructuralOnly(t *testing.T) {
	r := NewRegistry()
	// Require only the parser's structural types; schemas register domain types.
	assert.Len(t, r.types, 3)
	for _, name := range []string{"word", "rest", "uint"} {
		_, ok := r.Get(name)
		assert.True(t, ok, "structural builtin %q should exist", name)
	}
	for _, name := range []string{"ifname", "ipv4", "vlan", "asn"} {
		_, ok := r.Get(name)
		assert.False(t, ok, "domain type %q must not be builtin", name)
	}
}

func TestRegisterAndGet(t *testing.T) {
	// The zero Registry must behave like a constructed one, built-ins included.
	for name, r := range map[string]*Registry{"constructed": NewRegistry(), "zero": {}} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, r.Register(Type{Name: "even", Pattern: `\d+`}))
			_, ok := r.Get("even")
			assert.True(t, ok)
			_, ok = r.Get("word")
			assert.True(t, ok)
		})
	}
}

func TestRegisterRejectsMalformed(t *testing.T) {
	for _, tc := range []struct {
		why string
		vt  Type
	}{
		{"empty name", Type{Name: "", Pattern: `\d+`}},
		{"empty pattern", Type{Name: "x", Pattern: ""}},
		{"bad regex", Type{Name: "x", Pattern: `(`}},
		{"text-anchored", Type{Name: "x", Pattern: `^\d+$`}},
		{"text-anchored escapes", Type{Name: "x", Pattern: `\A\d+\z`}},
		{"line-anchored", Type{Name: "x", Pattern: `(?m)^\d+`}},
		{"capturing group", Type{Name: "x", Pattern: `a(\d+)`}},
	} {
		assert.Error(t, NewRegistry().Register(tc.vt), tc.why)
	}
	// The non-capturing form of the rejected group is accepted.
	ok := Type{Name: "x", Pattern: `a(?:\d+)`}
	assert.NoError(t, NewRegistry().Register(ok))
}
