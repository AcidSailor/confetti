// Package validate implements two phases of semantic validation. Phase A runs per line
// during Import: value-type checks on captured fields (list args validate
// per item, as the author spelled them), cardinality and duplicate-key
// detection, toggle co-presence, and required-child presence. Phase B
// (CommitCheck) runs over an assembled tree: referential integrity for every
// Ref (per item for list-valued refs) and Requires existence.
//
// Invalid values, unresolved references, and duplicate keys are Errors in all
// policies. diag.Policy controls only unknown input during parsing. An
// unresolved-reference diagnostic identifies the referring source line.
package validate
