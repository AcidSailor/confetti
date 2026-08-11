package value

import (
	"fmt"
	"regexp/syntax"
	"slices"
)

// Type defines an unanchored, non-capturing match pattern and optional validation for a captured string.
type Type struct {
	Name    string
	Pattern string
	Check   func(s string) error // nil => the pattern alone fully validates the value
}

// Registry maps type names to Types; the zero Registry gains the built-ins on first Register.
type Registry struct {
	types map[string]Type
}

// NewRegistry returns a registry preloaded with the built-in value types.
func NewRegistry() *Registry {
	r := &Registry{types: map[string]Type{}}
	for _, t := range builtins() {
		r.types[t.Name] = t
	}
	return r
}

// anchorOps are the ops that break a pattern once it is embedded in a larger expression.
var anchorOps = []syntax.Op{
	syntax.OpBeginText, syntax.OpEndText,
	syntax.OpBeginLine, syntax.OpEndLine,
}

// Register adds or replaces a valid type; callers must register all types before compiling schema templates.
func (r *Registry) Register(t Type) error {
	if t.Name == "" {
		return fmt.Errorf("value type has empty name")
	}
	if t.Pattern == "" {
		return fmt.Errorf("value type %q has empty pattern", t.Name)
	}
	re, err := syntax.Parse(t.Pattern, syntax.Perl)
	if err != nil {
		return fmt.Errorf("value type %q has invalid pattern: %w", t.Name, err)
	}
	if hasOp(re, anchorOps...) {
		return fmt.Errorf("value type %q anchors its pattern", t.Name)
	}
	if hasOp(re, syntax.OpCapture) {
		return fmt.Errorf("value type %q captures a group; use (?:...)", t.Name)
	}
	if r.types == nil {
		*r = *NewRegistry()
	}
	r.types[t.Name] = t
	return nil
}

// Get returns the named type and whether it is registered.
func (r *Registry) Get(name string) (Type, bool) {
	t, ok := r.types[name]
	return t, ok
}

// hasOp reports whether re or any subexpression uses one of the given ops.
func hasOp(re *syntax.Regexp, ops ...syntax.Op) bool {
	if slices.Contains(ops, re.Op) {
		return true
	}
	for _, sub := range re.Sub {
		if hasOp(sub, ops...) {
			return true
		}
	}
	return false
}

func builtins() []Type {
	return []Type{
		{Name: "word", Pattern: `\S+`},
		{Name: "rest", Pattern: `.+`}, // greedy; lazified when non-terminal
		{Name: "uint", Pattern: `\d+`},
	}
}
