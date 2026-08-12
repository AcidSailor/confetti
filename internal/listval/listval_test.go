package listval

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name, raw string
		want      []string
	}{
		{"single", "10", []string{"10"}},
		{"commas sorted numerically", "20,3,10", []string{"3", "10", "20"}},
		{"range expands", "10,30-33", []string{"10", "30", "31", "32", "33"}},
		{"dedupe incl range overlap", "10,10,9-11", []string{"9", "10", "11"}},
		{"single-item range", "7-7", []string{"7"}},
		{"non-numeric lexical", "beta,alpha", []string{"alpha", "beta"}},
		{
			"dash token is one item, not a range",
			"ge-0/0/1,xe-1",
			[]string{"ge-0/0/1", "xe-1"},
		},
		{
			"numeric before non-numeric",
			"b,2,a,10",
			[]string{"2", "10", "a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Expand(tt.raw, "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandErrors(t *testing.T) {
	for _, raw := range []string{
		"",            // empty list
		"10,,20",      // empty element
		"10,",         // trailing comma
		"30-20",       // reversed range
		"10-",         // open range
		"-20",         // open range
		"1-100000000", // range too large
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := Expand(raw, "")
			assert.Error(t, err)
		})
	}
}

func TestExpandRangeEndingAtMaxInt(t *testing.T) {
	hi := int(^uint(0) >> 1)
	lo := hi - 7
	got, err := Expand(strconv.Itoa(lo)+"-"+strconv.Itoa(hi), "")
	require.NoError(t, err)
	require.Len(t, got, 8)
	assert.Equal(t, strconv.Itoa(lo), got[0])
	assert.Equal(t, strconv.Itoa(hi), got[7])

	_, err = Expand("0-"+strconv.Itoa(hi), "")
	assert.Error(t, err)
}

func TestCompress(t *testing.T) {
	tests := []struct {
		name  string
		items []string
		want  string
	}{
		{"folds runs", []string{"10", "11", "12", "20"}, "10-12,20"},
		{"two-run folds", []string{"10", "11"}, "10-11"},
		{
			"unsorted dupes canonicalize",
			[]string{"12", "10", "12", "11"},
			"10-12",
		},
		{"non-numeric never folds", []string{"beta", "alpha"}, "alpha,beta"},
		{"mixed", []string{"b", "2", "3", "a"}, "2-3,a,b"},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Compress(tt.items, ""))
		})
	}
}

func TestCompressZeroPaddedRoundTrip(t *testing.T) {
	// Zero-padded spellings never fold into ranges ("007-9" would re-expand
	// to 7,8,9) and dedupe against their canonical twin by value.
	out := Compress([]string{"007", "008", "9"}, "")
	assert.Equal(t, "007,008,9", out)
	back, err := Expand(out, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"007", "008", "9"}, back)

	out = Compress([]string{"7", "007"}, "")
	assert.Equal(t, "007", out) // one value, deterministic spelling
	back, err = Expand(out, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"007"}, back)
}

func TestCompressExpandRoundTrip(t *testing.T) {
	items, err := Expand("40,10,20-22,10", "")
	require.NoError(t, err)
	assert.Equal(t, "10,20-22,40", Compress(items, ""))
	back, err := Expand(Compress(items, ""), "")
	require.NoError(t, err)
	assert.Equal(t, items, back)
}

func TestExpandCompressCustomSep(t *testing.T) {
	items, err := Expand("40 10 20-22", " ")
	require.NoError(t, err)
	assert.Equal(t, []string{"10", "20", "21", "22", "40"}, items)
	assert.Equal(t, "10,20-22,40", Compress(items, ","))
	assert.Equal(t, "10 20-22 40", Compress(items, " "))
}

var kwTrunk = Keywords{
	None:   "none",
	All:    "all",
	Except: "except",
	Domain: "1-6",
}

func TestResolveKeywords(t *testing.T) {
	none, err := Resolve("none", "", kwTrunk)
	require.NoError(t, err)
	assert.Empty(t, none)

	all, err := Resolve("all", "", kwTrunk)
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2", "3", "4", "5", "6"}, all)

	exc, err := Resolve("except 2-3", "", kwTrunk)
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "4", "5", "6"}, exc)

	plain, err := Resolve("2,4", "", kwTrunk)
	require.NoError(t, err)
	assert.Equal(t, []string{"2", "4"}, plain)

	// Undeclared keywords stay ordinary (mis)items.
	_, err = Resolve("none", "", Keywords{})
	require.NoError(t, err) // "none" is just a lexical item without keywords
}

func TestResolveExceptMalformed(t *testing.T) {
	_, err := Resolve("except 9-5", "", kwTrunk)
	assert.Error(t, err)
	// A bare except word with no list is not the keyword form: one item.
	items, err := Resolve("except", "", kwTrunk)
	require.NoError(t, err)
	assert.Equal(t, []string{"except"}, items)
}

func TestPartsNamesWhatAuthorWrote(t *testing.T) {
	// Keyword spellings name no items; the Except form names its exceptions.
	for _, raw := range []string{"none", "all"} {
		items, err := Parts(raw, "", kwTrunk)
		require.NoError(t, err)
		assert.Empty(t, items)
	}
	exc, err := Parts("except 2-3", "", kwTrunk)
	require.NoError(t, err)
	assert.Equal(t, []string{"2", "3"}, exc)
	plain, err := Parts("2,4", "", kwTrunk)
	require.NoError(t, err)
	assert.Equal(t, []string{"2", "4"}, plain)
}

func TestCanonicalPrefersKeywords(t *testing.T) {
	assert.Equal(t, "none", Canonical(nil, "", kwTrunk))
	assert.Equal(
		t,
		"all",
		Canonical([]string{"1", "2", "3", "4", "5", "6"}, "", kwTrunk),
	)
	assert.Equal(t, "2,4", Canonical([]string{"4", "2"}, "", kwTrunk))
	// Without keywords: empty has no spelling, full domain stays explicit.
	assert.Equal(t, "", Canonical(nil, "", Keywords{}))
	assert.Equal(t, "1-3", Canonical([]string{"1", "2", "3"}, "", Keywords{}))
}

func TestMembersIntersectWithoutExpandingRanges(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
		why  string
	}{
		{a: "1-65536", b: "32768-70000", want: true, why: "overlapping ranges"},
		{a: "1-100", b: "101-200", want: false, why: "adjacent but disjoint"},
		{a: "1-3,blue", b: "blue,9", want: true, why: "shared lexical item"},
		{a: "red", b: "blue", want: false, why: "distinct lexical items"},
		{a: "", b: "", want: false, why: "empty sets hold nothing"},
		{a: "", b: "1-3", want: false, why: "empty set against a range"},
		// Expand deduplicates "007" and "7" as one item, so intersection must agree.
		{a: "007", b: "7", want: true, why: "zero-padded spelling"},
		{a: "007", b: "1-10", want: true, why: "zero-padded inside a range"},
		{a: "0070", b: "1-10", want: false, why: "zero-padded outside a range"},
	}
	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			a, b := members(t, tt.a), members(t, tt.b)
			assert.Equal(t, tt.want, a.Intersects(b))
			// Intersection is symmetric regardless of how each side was spelled.
			assert.Equal(t, tt.want, b.Intersects(a))
		})
	}
}

// members expands a raw list into comparable members; an empty value holds none.
func members(t *testing.T, raw string) Members {
	t.Helper()
	if raw == "" {
		return nil
	}
	items, err := Expand(raw, "")
	require.NoError(t, err)
	return Intervals(items)
}
