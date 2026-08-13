// Package merge combines configuration fragments from left to right without
// modifying them. It removes duplicate lines and recursively merges sections.
// Merge does not run a commit check.
//
// The Options resolver handles contested slots. Declared applies the schema
// strategy, unions lists, merges matching sections, and otherwise keeps the
// later value. Refuse rejects conflicts not resolved by the schema or list
// union. A custom resolver receives the declared schema strategy.
//
// Refused reports an Error and keeps the earlier value. Overridden and
// value-changing Combined outcomes report Warnings. Merge rejects resolver
// output that breaks slot identity or tree ownership.
//
// Merge treats each non-keyed ZeroToOne definition as one slot per level.
// Remediate requires a shared Kind to pair sibling definitions because
// unmarked siblings can be independent.
package merge
