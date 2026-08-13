// Package merge assembles configuration fragments from left to right in a new
// tree. It removes duplicate lines and recursively merges sections. A slot
// claimed twice with different values goes to the Options resolver: Declared
// (the default) follows the slot's schema merge strategy, including a
// definition's MergeFunc, unions list slots, and keeps the later value
// otherwise; Refuse reports an Error for everything the schema does not
// sanction; KeepFirst and KeepLast ignore the schema. The
// severity follows the outcome: Refused is an Error and keeps the earlier
// value, Overridden and a value-changing Combined are Warnings, and a
// Combined that hands back the earlier node warns that a leaf's later value
// was dropped. A resolver-built node that breaks the identity contract is an
// Error that keeps the earlier value. Merge does not run a commit check.
//
// Merge treats each non-keyed ZeroToOne definition as one slot per level.
// Remediate requires a shared Kind to pair sibling definitions because
// unmarked siblings can be independent.
package merge
