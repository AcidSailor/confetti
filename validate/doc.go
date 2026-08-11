// Package validate provides import-time and commit-time semantic checks.
// ImportCheck validates values, cardinality, duplicate keys, toggles, and
// required children at each tree level. CommitCheck validates references and
// Requires constraints against an assembled tree.
//
// Invalid values, unresolved references, and duplicate keys are Errors in all
// policies. diag.Policy controls only unknown input during parsing. An
// unresolved-reference diagnostic identifies the referring source line.
package validate
