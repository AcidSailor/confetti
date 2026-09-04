package ident

import (
	"testing"

	"github.com/acidsailor/confetti/schema"
	"github.com/stretchr/testify/assert"
)

// The identity helpers are total: an absent node or definition names nothing.
func TestScopeHelpersTolerateMissingDefinitions(t *testing.T) {
	unmatched := schema.NewNode("who knows")
	for _, n := range []*schema.Node{nil, unmatched} {
		assert.Equal(t, Scope{}, ScopeOf(n, PerOwner))
		assert.Equal(t, Scope{}, ScopeOf(n, Device))
		assert.Empty(t, OwnerKey(n))
		assert.Empty(t, KeyValue(n))
	}
}

func TestScopeStringNamesLabelThenDefinition(t *testing.T) {
	def := schema.New().Node("slot {{ id:word }}").Key("id")
	assert.Equal(t, `label "acl"`, Scope{Label: "acl"}.String())
	assert.Equal(t, `definition "slot {{ id:word }}"`, Scope{Def: def}.String())
	assert.Equal(t, "an unnamed scope", Scope{}.String())
}

// A declared anchor that never encloses the node falls back to the root space.
func TestScopeOfOutsideEveryAnchorUsesTheRootSpace(t *testing.T) {
	s := schema.New()
	box := s.Node("box {{ b:word }}").Card(schema.ZeroToN).Kind("box").Key("b")
	claim := s.Node("claim {{ id:word }}").Card(schema.ZeroToN).
		Kind("claim").Key("id").ScopedBy(box)

	cfg := schema.NewConfig(s)
	n := schema.NewNode("claim 1")
	n.Def, n.Fields = claim, map[string]string{"id": "1"}
	cfg.Root.AddChild(n)

	assert.Equal(t, Scope{Label: "claim"}, ScopeOf(n, PerOwner))
}
