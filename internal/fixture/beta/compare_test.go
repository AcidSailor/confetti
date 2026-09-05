package beta

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareVlanStateFlip(t *testing.T) {
	// Compare shows both states even though the remediation artifact emits only the new line.
	e := Engine()
	run, d1 := e.Import(fmt.Sprintf(vlanTmpl, "state enable"))
	require.False(t, d1.HasErrors())
	intd, d2 := e.Import(fmt.Sprintf(vlanTmpl, "state disable"))
	require.False(t, d2.HasErrors())

	out, d := e.Compare(run, intd)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"  vlan database\n"+
			"-   vlan 10 bridge 1 state enable\n"+
			"+   vlan 10 bridge 1 state disable\n",
		out)
}

func TestCompareVlanRenameShowsPair(t *testing.T) {
	e := Engine()
	run, d1 := e.Import(fmt.Sprintf(vlanTmpl, "state enable"))
	require.False(t, d1.HasErrors())
	intd, d2 := e.Import(fmt.Sprintf(vlanTmpl, "name CORE state enable"))
	require.False(t, d2.HasErrors())

	out, d := e.Compare(run, intd)
	require.False(t, d.HasErrors(), d.String())
	assert.Equal(t,
		"  vlan database\n"+
			"-   vlan 10 bridge 1 state enable\n"+
			"+   vlan 10 bridge 1 name CORE state enable\n",
		out)
}
