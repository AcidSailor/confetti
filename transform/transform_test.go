package transform

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDropLines(t *testing.T) {
	// Matching lines are BLANKED, not removed: the parser skips blanks, so
	// the tree is identical, but line numbering stays aligned with the
	// original text (diagnostics carry source lines).
	r, err := DropLines(`^!`)
	require.NoError(t, err)
	in := "!comment\ninterface Ethernet1/1\n! another\n  shutdown\n"
	out := ApplyText([]TextRule{r}, in)
	assert.Equal(t, "\ninterface Ethernet1/1\n\n  shutdown\n", out)
	assert.Equal(t, strings.Count(in, "\n"), strings.Count(out, "\n"))
}

func TestPerLineSub(t *testing.T) {
	r, err := PerLineSub(` {2,}`, " ")
	require.NoError(t, err)
	assert.Equal(
		t,
		"ip address 10.0.0.1 255.255.255.0",
		r.Apply("ip address 10.0.0.1    255.255.255.0"),
	)
}

func TestApplyTextPipeline(t *testing.T) {
	drop, _ := DropLines(`^!`)
	collapse, _ := PerLineSub(` {2,}`, " ")
	out := ApplyText([]TextRule{drop, collapse}, "!x\nfoo   bar\n")
	assert.Equal(t, "\nfoo bar\n", out)
}
