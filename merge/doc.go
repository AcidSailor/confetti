// Package merge assembles configuration fragments from left to right in a new
// tree. It removes duplicate lines and recursively merges sections. A slot with
// different values is a conflict. Strict mode reports an Error and keeps the
// first value. Lenient mode reports a Warning and keeps the last value. A
// schema List slot instead unions values without a diagnostic. Merge does not
// run a commit check.
//
// Merge treats each non-keyed ZeroToOne definition as one slot per level.
// Remediate requires a shared Kind to pair sibling definitions because
// unmarked siblings can be independent.
package merge
