// Package validate provides import-time and commit-time semantic checks.
// ImportCheck validates values, cardinality, duplicate keys, toggles, and
// required children at each tree level. CommitCheck validates references,
// prerequisites, and sibling exclusions against an assembled tree. Relations
// match Kind names and Tags. CommitCheck also rejects two objects that claim
// one name in the same space — the Namespace, else the Kind, else the
// definition — scoped per owner unless the schema declares otherwise, and
// reports invalid schema relations.
//
// Invalid values, unresolved references, and duplicate keys are Errors. An
// unresolved-reference diagnostic identifies the referring source line.
// Exclusive-name collisions are Errors. An unresolvable List arg downgrades to
// a Warning that skips exclusive-name checking for that object; a baseline
// object reports without a line, because its position is not the caller's.
package validate
