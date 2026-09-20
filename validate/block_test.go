package validate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// bannerTree builds a delimited-block node directly, the way a downstream
// package, a tree transform, or a merge resolver would. Parse cannot produce
// these bodies, so this is the only path that reaches BlockBodies.
func bannerTree(body []string) *schema.Config {
	s := schema.New()
	def := s.Node("banner motd {{ d:delim }}").
		Card(schema.ZeroToOne).BlockDelim("d")
	cfg := schema.NewConfig(s)
	n := cfg.Root.AddChild(schema.NewNode("banner motd ^"))
	n.Def, n.Fields, n.Block = def, map[string]string{"d": "^"}, body
	return cfg
}

func TestBlockBodiesRejectsEmptyBody(t *testing.T) {
	for name, body := range map[string][]string{
		"nil":            nil,
		"empty slice":    {},
		"one empty line": {""},
	} {
		t.Run(name, func(t *testing.T) {
			d := diag.New()
			BlockBodies(bannerTree(body), d)
			require.True(t, d.HasErrors(), d.String())
			assert.Contains(t, d.String(), "delimited block has no body")
		})
	}
}

// Render would emit this body verbatim between two delimiters, and a device
// would close the block at the first one it met inside it.
func TestBlockBodiesRejectsDelimiterInBody(t *testing.T) {
	d := diag.New()
	BlockBodies(bannerTree([]string{" a ^ b "}), d)
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "contains its own delimiter")
}

func TestBlockBodiesAcceptsParsedShapes(t *testing.T) {
	for name, body := range map[string][]string{
		"one line":   {" hi "},
		"multi line": {"", "hi", ""},
		"blank body": {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			d := diag.New()
			BlockBodies(bannerTree(body), d)
			assert.Empty(t, d.Items, d.String())
		})
	}
}

// CommitCheck is the gate Remediate runs on an intended tree, so the rule has to
// fire there and not only through the standalone helper.
func TestCommitCheckReportsEmptyDelimitedBody(t *testing.T) {
	cfg := bannerTree(nil)
	d := diag.New()
	CommitCheck(cfg, nil, d)
	require.True(t, d.HasErrors(), d.String())
	assert.Contains(t, d.String(), "delimited block has no body")
}
