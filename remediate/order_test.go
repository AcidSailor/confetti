package remediate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildOrderIndexDeclarationOrder(t *testing.T) {
	s := testSchema()
	idx := buildOrderIndex(s)
	vlan := s.Roots[0]
	iface := s.Roots[1]
	assert.Less(t, idx[vlan], idx[iface])
	assert.Less(t, idx[iface], idx[iface.Children[0]])
}
