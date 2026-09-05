// Package validate provides import-time and commit-time semantic checks.
// ImportCheck validates values, cardinality, duplicate keys, toggles, and
// required children at each tree level. CommitCheck validates references,
// prerequisites, and sibling exclusions against an assembled tree. Relations
// match Kind names and Tags. CommitCheck also validates schema relations and
// rejects duplicate claims on an exclusive name. The name space is the
// Namespace, Kind, or definition and defaults to one space per owner.
//
// Invalid values, unresolved references, and duplicate keys are Errors. An
// unresolved-reference diagnostic identifies the referring source line.
// Exclusive-name collisions are Errors. An unresolvable List arg produces a
// Warning and skips that claim. Baseline diagnostics omit source lines.
package validate
