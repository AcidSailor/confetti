// Package diag defines Error and Warning diagnostics with optional 1-based
// source lines. Each stage appends diagnostics; callers use HasErrors to check
// the complete result.
//
// Policy.Strict is the engine-wide strictness knob. Each stage documents the
// recovery strategy it selects; a lenient stage may downgrade a severity, drop
// unknown input, or resolve a conflict instead of refusing. No policy relaxes
// constraints on schema-known input. Diagnostics omit a line number when no
// single source line identifies the problem.
package diag
