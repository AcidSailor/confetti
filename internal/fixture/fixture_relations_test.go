package fixture_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/fixture/alpha"
	"github.com/acidsailor/confetti/internal/fixture/beta"
	"github.com/acidsailor/confetti/schema"
)

// TestFixtureSchemasDeclareResolvableRelations guards the only in-repository grammars against an unresolvable relation.
func TestFixtureSchemasDeclareResolvableRelations(t *testing.T) {
	for name, s := range map[string]*schema.Schema{
		"alpha": alpha.Schema(), "beta": beta.Schema(),
	} {
		d := diag.New()
		s.ValidateRelations(d)
		assert.False(t, d.HasErrors(), "%s: %s", name, d.String())
	}
}
