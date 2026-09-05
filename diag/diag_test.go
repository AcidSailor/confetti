package diag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectAndHasErrors(t *testing.T) {
	d := New()
	assert.False(t, d.HasErrors())
	d.Add(Warning, "dropped %d nodes", 3)
	assert.False(t, d.HasErrors())
	d.Add(Error, "vlan %q does not exist", "10")
	assert.True(t, d.HasErrors())
	assert.Len(t, d.Items, 2)
}

func TestSeverityString(t *testing.T) {
	assert.Equal(t, "warning", Warning.String())
	assert.Equal(t, "error", Error.String())
	assert.Equal(t, "Severity(7)", Severity(7).String())
}

func TestStringFormat(t *testing.T) {
	d := New()
	d.Add(Error, "boom")
	assert.Contains(t, d.String(), "error: boom")
}

func TestMerge(t *testing.T) {
	a := New()
	a.Add(Warning, "first")
	b := New()
	b.Add(Error, "second")
	b.Add(Warning, "third")

	a.Merge(b)

	items := a.Items
	require.Len(t, items, 3)
	assert.Equal(t, Diagnostic{Severity: Warning, Message: "first"}, items[0])
	assert.Equal(t, Diagnostic{Severity: Error, Message: "second"}, items[1])
	assert.Equal(t, Diagnostic{Severity: Warning, Message: "third"}, items[2])
	assert.True(t, a.HasErrors())
	// Merge must preserve the source diagnostics.
	assert.Len(t, b.Items, 2)
}

func TestMergePreservesPercentLiteral(t *testing.T) {
	a := New()
	b := New()
	b.Add(Error, "100%% done") // stored verbatim as "100% done"
	a.Merge(b)
	assert.Equal(t, "100% done", a.Items[0].Message)
}

func TestAddAtCarriesLine(t *testing.T) {
	d := New()
	d.AddAt(12, Error, "boom %d", 1)
	d.AddAt(0, Warning, "nowhere")
	d.AddAt(-3, Warning, "clamped")
	items := d.Items
	assert.Equal(t, 12, items[0].Line)
	assert.Equal(t, "boom 1", items[0].Message)
	assert.Equal(t, 0, items[1].Line)
	assert.Equal(t, 0, items[2].Line, "negative degrades to unpositioned")
	assert.Contains(t, d.String(), "12: error: boom 1")
	assert.Contains(t, d.String(), "warning: nowhere")
	assert.NotContains(t, d.String(), "0: warning")
}
