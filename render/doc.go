// Package render converts a schema.Config to canonical text. Matched nodes use
// their schema definition with normalized spacing and two-space indentation.
// Unmatched nodes use their raw text. Raw block bodies are preserved exactly.
// A BlockDelim body is joined between the opener's delimiter and a re-emitted
// one, so a banner comes out on one line or many according to its own text; an
// empty body is omitted. A schema.SectionExit token closes each section that
// declares one.
//
// The round-trip contract is render(parse(x)) == canonical(x); repeating
// it produces the same text. Remediation trees arrive already scheduled and
// use the same rendering path. The renderer ignores operation tags.
package render
