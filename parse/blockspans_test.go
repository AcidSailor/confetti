package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/acidsailor/confetti/schema"
)

func TestBlockSpansNilWithoutBlockDefs(t *testing.T) {
	assert.Nil(t, BlockSpans(miniSchema(), "interface Ethernet1/1\n"))
}

func TestBlockSpansRootBlock(t *testing.T) {
	spans := BlockSpans(
		blockSchema(),
		"interface eth1\n  mtu 9000\nbanner motd ^\nhello\n\n^\ninterface eth2\n",
	)
	assert.Equal(
		t,
		[]bool{false, false, true, true, true, true, false, false},
		spans, // trailing "" from the final \n is outside
	)
}

func TestBlockSpansUnterminatedRunsToEOF(t *testing.T) {
	spans := BlockSpans(blockSchema(), "banner motd ^\nhello\nworld")
	assert.Equal(t, []bool{true, true, true}, spans)
}

func TestBlockSpansNestedLineIsNotAnOpener(t *testing.T) {
	// A nested line matching a root block-opener template must not open a
	// span: at interface level the banner def is not reachable.
	spans := BlockSpans(
		blockSchema(),
		"interface eth1\n  banner motd ^\n  mtu 9000\ninterface eth2\n",
	)
	assert.Equal(t, []bool{false, false, false, false, false}, spans)
}

func TestBlockSpansNestedBlockDefMatchesAtItsLevel(t *testing.T) {
	s := schema.New()
	iface := s.Node("interface {{ name:word }}").Card(schema.ZeroToN)
	iface.Child("banner login {{ delim:word }}").
		Card(schema.ZeroToOne).BlockDelim("delim")
	spans := BlockSpans(
		s,
		"interface eth1\n  banner login ^\nsecret\n^\ninterface eth2\n"+
			"banner login ^\n",
	)
	// The nested def protects its span; the same text at root is unreachable.
	assert.Equal(t, []bool{false, true, true, true, false, false, false}, spans)
}

func TestBlockSpansUnknownParentIsolatesLikeParse(t *testing.T) {
	// The guard and parser must both ignore an opener nested under an unknown line.
	spans := BlockSpans(
		blockSchema(),
		"frobnicate\n  banner motd ^\nbanner motd ^\nbody\n^\n",
	)
	assert.Equal(t, []bool{false, false, true, true, true, false}, spans)
}

func TestBlockSpansTerminatorTrailingWhitespace(t *testing.T) {
	spans := BlockSpans(blockSchema(), "banner motd ^\nbody\n^  \nmore")
	assert.Equal(t, []bool{true, true, true, false}, spans)
}
