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
	// roots numbered in declaration order; vlan before interface
	assert.Less(t, idx[vlan], idx[iface])
	// a child has a higher index than its root parent (pre-order walk)
	assert.Less(t, idx[iface], idx[iface.Children[0]])
}
