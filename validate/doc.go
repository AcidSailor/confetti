// Package validate provides import-time and commit-time semantic checks.
// ImportCheck validates values, cardinality, duplicate keys, toggles, and
// required children at each tree level. CommitCheck validates references,
// prerequisites, and sibling exclusions against an assembled tree. Relations
// match Kind names and Tags. CommitCheck also rejects two objects that claim
// one name under the same Namespace or Kind, and reports invalid schema
// relations.
//
// Invalid values, unresolved references, and duplicate keys are Errors. An
// unresolved-reference diagnostic identifies the referring source line.
package validate
