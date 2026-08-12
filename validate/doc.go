// Package validate provides import-time and commit-time semantic checks.
// ImportCheck validates values, cardinality, duplicate keys, toggles, and
// required children at each tree level. CommitCheck validates relations
// (references, Requires prerequisites, and tag exclusions) against an
// assembled tree. A relation matches labels: a node's Kind name plus its
// Tags. Sibling-scope relations compare direct children of one parent only.
//
// Invalid values, unresolved references, and duplicate keys are Errors in all
// policies. diag.Policy controls only unknown input during parsing. An
// unresolved-reference diagnostic identifies the referring source line.
package validate
