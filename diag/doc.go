// Package diag defines Error and Warning diagnostics with optional 1-based
// source lines. Each stage appends diagnostics; callers use HasErrors to check
// the complete result.
//
// Recovery strategies belong to the stages that emit diagnostics and do not
// relax constraints on schema-known input. Diagnostics omit a line number when
// no single source line identifies the problem.
package diag
