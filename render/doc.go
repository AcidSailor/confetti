// Package render converts a schema.Config to canonical text. Matched nodes use
// their schema definition with normalized spacing and two-space indentation.
// Unmatched nodes use their raw text. Raw block bodies are preserved exactly
// and the terminator is re-emitted from the node's fields. An empty BlockDelim
// body closes on the opening line when schema.Node.InlineBlock confirms the
// round trip. A schema.SectionExit token closes each section that declares one.
//
// The round-trip contract is render(parse(x)) == canonical(x); repeating
// it produces the same text. Remediation trees arrive already scheduled and
// use the same rendering path. The renderer ignores operation tags.
package render
