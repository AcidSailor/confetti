// Package render converts a tree.Config to canonical text. Matched nodes use
// their schema definition with normalized spacing and two-space indentation.
// Unmatched nodes use their raw text. Raw block bodies and terminators are
// preserved exactly. A schema.SectionExit token closes each section that
// declares one.
//
// The round-trip contract is canonical, not byte-exact: render(parse(x))
// == canonical(x), and canonical is a fixed point. Remediation trees arrive
// already scheduled by the remediate package and use the same rendering path.
// The renderer ignores operation tags.
package render
