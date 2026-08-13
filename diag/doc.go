// Package diag defines Error and Warning diagnostics with optional 1-based
// source lines. Each stage appends diagnostics; callers use HasErrors to check
// the complete result.
//
// Recovery strategies live with the stages they govern: parse.Unknown selects
// unknown-input handling, merge.Options selects conflict resolution, and
// remediate.Cycle selects ordering-cycle handling. No option relaxes
// constraints on schema-known input. Diagnostics omit a line number when no
// single source line identifies the problem.
package diag
