// Package lcp computes the longest-common-prefix length of ordered paths.
package lcp

// Len returns the number of leading elements a and b share.
func Len[T comparable](a, b []T) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}
