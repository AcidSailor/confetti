// Package merge assembles configuration fragments from left to right in a new
// tree. It removes duplicate lines and recursively merges sections. A slot with
// different values is a conflict. Strict mode reports an Error and keeps the
// first value. Lenient mode reports a Warning and keeps the last value. A
// schema List slot instead unions values without a diagnostic. Merge does not
// run a commit check.
//
// Merge treats each non-keyed ZeroToOne definition as one slot per level,
// regardless of value. Therefore, "router bgp 65000" and "router bgp 65001"
// conflict. Remediate applies this pairing only where the schema marks a slot
// with a Kind, because an unmarked pair can be genuinely independent
// siblings; it handles the unmarked difference as Remove and Add operations
// and reports the split.
package merge
