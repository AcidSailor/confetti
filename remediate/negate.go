package remediate

import (
	"fmt"
	"strings"

	"github.com/acidsailor/confetti/schema"
)

// negateLine applies a definition's NegateStrategy to a rendered positive line.
func negateLine(
	def *schema.Node,
	fields map[string]string,
	rendered string,
) string {
	if def == nil {
		// Use "no" for unmatched nodes created outside Import.
		return noPrefix(rendered, "no")
	}
	ns := def.Negate
	switch ns.Kind {
	case schema.NegFunc:
		return ns.Func(fields, rendered)
	case schema.NegTemplate:
		return schema.Interpolate(ns.Template, fields)
	case schema.NegDefault:
		return "default " + rendered
	case schema.NegNoPrefix:
		return noPrefix(rendered, def.Schema.NegationWord)
	default:
		// Reject unknown strategies instead of producing an unsafe default command.
		panic(fmt.Sprintf("remediate: unknown NegateKind %d", ns.Kind))
	}
}

// noPrefix removes an existing negation word or adds it when absent.
func noPrefix(rendered, word string) string {
	if rest, ok := strings.CutPrefix(rendered, word+" "); ok {
		return rest
	}
	return word + " " + rendered
}
