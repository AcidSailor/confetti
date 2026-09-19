// Package parse converts configuration text to a schema.Config. It uses an
// indentation stack and schema.MatchChild. An unknown line creates an isolated
// stack frame for its children. Fold canonicalizes alternate spellings after
// parsing. At each level, it applies RespellAs, list continuations, and
// membership expansion in that order.
//
// Raw block body lines, including blank lines, are preserved byte-exact.
// BlockSpans marks the opener through the terminator using the same indentation
// rules, so text transforms can exclude blocks without building a tree.
//
// A BlockDelim block runs from its captured one-character delimiter to the next
// occurrence of that delimiter, on the opening line or any later one. Block holds the body
// split on newlines, so it carries the text beside each delimiter and is never
// normalized. Text after the closing delimiter reports an Error, and a body that
// is empty is dropped, as is a block that never closed, because a device omits
// the first and never reports the second.
//
// A Fold change is atomic for each line. Each synthesized node matches its
// rendered text against all definitions at the level with schema.MatchChild.
// Matching only the intended definition could select an equal-specificity
// sibling when the rendered result is parsed again.
package parse
