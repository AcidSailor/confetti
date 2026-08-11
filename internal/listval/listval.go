package listval

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// maxRange limits range expansion to prevent excessive memory use from invalid input.
const maxRange = 1 << 16

// sepOr returns a comma when no separator is declared.
func sepOr(sep string) string {
	if sep == "" {
		return ","
	}
	return sep
}

// Keywords defines optional vendor spellings and the raw domain used by All and Except.
type Keywords struct {
	None, All, Except string
	Domain            string
}

// domains memoizes domain expansions, which are schema-static and re-read per folded line.
// never evicted; schemas declare a fixed set of domains, so growth is bounded.
var domains sync.Map

// domain returns the expanded keyword domain. Callers must not modify the result.
func domain(raw, sep string) ([]string, error) {
	key := raw + "\x00" + sep
	if v, ok := domains.Load(key); ok {
		return v.([]string), nil
	}
	items, err := Expand(raw, sep)
	if err != nil {
		return nil, err
	}
	domains.Store(key, items)
	return items, nil
}

// Expand parses a separated list, expands numeric ranges, removes duplicates, and sorts numeric items before lexical items.
func Expand(raw, sep string) ([]string, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty list")
	}
	var items []string
	for tok := range strings.SplitSeq(raw, sepOr(sep)) {
		vals, err := expandTok(tok)
		if err != nil {
			return nil, err
		}
		items = append(items, vals...)
	}
	return normalize(items), nil
}

// Resolve parses a slot value as None, All, Except, or a plain list and returns its semantic item set.
func Resolve(raw, sep string, kw Keywords) ([]string, error) {
	switch {
	case kw.None != "" && raw == kw.None:
		return nil, nil
	case kw.All != "" && raw == kw.All:
		dom, err := domain(kw.Domain, sep)
		if err != nil {
			return nil, err
		}
		return slices.Clone(dom), nil // Callers append to the result.
	}
	if rest, ok := exceptRest(raw, kw); ok {
		dom, err := domain(kw.Domain, sep)
		if err != nil {
			return nil, err
		}
		exc, err := Expand(rest, sep)
		if err != nil {
			return nil, err
		}
		drop := make(map[string]bool, len(exc))
		for _, it := range exc {
			drop[it] = true
		}
		var out []string
		for _, it := range dom {
			if !drop[it] {
				out = append(out, it)
			}
		}
		return out, nil
	}
	return Expand(raw, sep)
}

// Parts returns explicit items for validation; Except returns its exceptions, and None or All returns no items.
func Parts(raw, sep string, kw Keywords) ([]string, error) {
	if (kw.None != "" && raw == kw.None) || (kw.All != "" && raw == kw.All) {
		return nil, nil
	}
	if rest, ok := exceptRest(raw, kw); ok {
		raw = rest
	}
	return Expand(raw, sep)
}

// Canonical returns None for an empty set, All for the complete domain, or a compressed explicit list.
func Canonical(items []string, sep string, kw Keywords) string {
	if len(items) == 0 {
		return kw.None
	}
	sorted := normalize(slices.Clone(items))
	if kw.All != "" && kw.Domain != "" {
		dom, err := domain(kw.Domain, sep)
		if err == nil && slices.Equal(sorted, dom) {
			return kw.All
		}
	}
	return compressSorted(sorted, sep)
}

// exceptRest reports raw as "<Except> <list>", returning the list part.
func exceptRest(raw string, kw Keywords) (string, bool) {
	if kw.Except == "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(raw, kw.Except+" ")
	return rest, ok && rest != ""
}

// Compress removes duplicates, sorts items, and forms numeric ranges without modifying the input; zero-padded values remain explicit.
func Compress(items []string, sep string) string {
	return compressSorted(normalize(slices.Clone(items)), sep)
}

// compressSorted folds runs of consecutive values in an already-normalized slice.
func compressSorted(items []string, sep string) string {
	var parts []string
	for i := 0; i < len(items); {
		lo, ok := plain(items[i])
		if !ok {
			parts = append(parts, items[i])
			i++
			continue
		}
		j, hi := i, lo
		for j+1 < len(items) {
			n, ok := plain(items[j+1])
			if !ok || n != hi+1 {
				break
			}
			hi = n
			j++
		}
		if j == i {
			parts = append(parts, items[i])
		} else {
			parts = append(parts, items[i]+"-"+items[j])
		}
		i = j + 1
	}
	return strings.Join(parts, sepOr(sep))
}

// expandTok expands a closed numeric range, rejects open and reversed ranges, and treats other text as one item.
func expandTok(tok string) ([]string, error) {
	if tok == "" {
		return nil, fmt.Errorf("empty element")
	}
	a, b, found := strings.Cut(tok, "-")
	na, aok := num(a)
	nb, bok := num(b)
	switch {
	case found && aok && bok:
		switch {
		case nb < na:
			return nil, fmt.Errorf("reversed range %q", tok)
		case nb-na >= maxRange:
			return nil, fmt.Errorf("range %q too large", tok)
		}
		out := make([]string, 0, nb-na+1)
		for v := na; ; v++ {
			out = append(out, strconv.Itoa(v))
			if v == nb {
				break
			}
		}
		return out, nil
	case found && ((aok && b == "") || (bok && a == "")):
		return nil, fmt.Errorf("open range %q", tok)
	}
	return []string{tok}, nil // Not a range: one item.
}

// normalize sorts and removes duplicates in place; numeric values sort before lexical values and deduplicate by numeric value.
func normalize(items []string) []string {
	slices.SortFunc(items, compare)
	return slices.CompactFunc(items, func(a, b string) bool {
		na, aok := num(a)
		nb, bok := num(b)
		if aok && bok {
			return na == nb
		}
		return a == b
	})
}

func compare(a, b string) int {
	na, aok := num(a)
	nb, bok := num(b)
	switch {
	case aok && bok:
		// Use lexical order to resolve equal numeric values such as "7" and "007".
		return cmp.Or(cmp.Compare(na, nb), cmp.Compare(a, b))
	case aok:
		return -1
	case bok:
		return 1
	default:
		return cmp.Compare(a, b)
	}
}

// num parses an unsigned decimal integer and rejects signs, non-digits, and overflow.
func num(s string) (int, bool) {
	n, err := strconv.ParseUint(s, 10, strconv.IntSize-1)
	return int(n), err == nil
}

// plain reports the value of a canonically spelled integer; zero-padded spellings are not canonical.
func plain(s string) (int, bool) {
	n, ok := num(s)
	return n, ok && (len(s) == 1 || s[0] != '0')
}
