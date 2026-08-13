package diag

import (
	"fmt"
	"slices"
	"strings"
)

// Severity classifies a diagnostic as Warning or Error.
type Severity int

const (
	Warning Severity = iota // The operation continued.
	Error                   // The result is incomplete or unusable.
)

// String returns "warning"/"error", or "Severity(n)" for an unknown value.
func (s Severity) String() string {
	switch s {
	case Warning:
		return "warning"
	case Error:
		return "error"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Diagnostic contains one severity, message, and optional source line.
type Diagnostic struct {
	Severity Severity
	Message  string
	// Line is the 1-based Import source line, or zero when no single line identifies the problem.
	Line int
}

// Diagnostics collects all diagnostics without stopping at the first Error.
type Diagnostics struct {
	Items []Diagnostic
}

// New returns an empty collector.
func New() *Diagnostics { return &Diagnostics{} }

// Add records an unpositioned diagnostic with a Sprintf-formatted message.
func (d *Diagnostics) Add(sev Severity, format string, args ...any) {
	d.AddAt(0, sev, format, args...)
}

// AddAt records a diagnostic at a 1-based source line and clamps a negative line to zero.
func (d *Diagnostics) AddAt(
	line int,
	sev Severity,
	format string,
	args ...any,
) {
	msg := fmt.Sprintf(format, args...)
	d.Items = append(
		d.Items,
		Diagnostic{Severity: sev, Message: msg, Line: max(line, 0)},
	)
}

// Merge appends diagnostics from o without reformatting their messages.
func (d *Diagnostics) Merge(o *Diagnostics) {
	d.Items = append(d.Items, o.Items...)
}

// HasErrors reports whether any collected diagnostic is an Error.
func (d *Diagnostics) HasErrors() bool {
	return slices.ContainsFunc(d.Items, func(it Diagnostic) bool {
		return it.Severity == Error
	})
}

// String renders one diagnostic per line, prefixed by the source line when known.
func (d *Diagnostics) String() string {
	var b strings.Builder
	for _, it := range d.Items {
		if it.Line > 0 {
			fmt.Fprintf(&b, "%d: %s: %s\n", it.Line, it.Severity, it.Message)
		} else {
			fmt.Fprintf(&b, "%s: %s\n", it.Severity, it.Message)
		}
	}
	return b.String()
}
