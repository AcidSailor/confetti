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
// A BlockDelim block can close on its opening line if removing the terminator
// leaves text that matches the same definition and delimiter. The parser removes
// trailing terminators while that match holds, keeping canonical output
// idempotent. Inline text stays in the opener's trailing capture and is normalized;
// Block is empty. Render puts the terminator on a separate line. Inline close
// requires a trailing text capture. An invalid close warns and leaves the block
// open if the opener ends with a terminator and contains another occurrence.
//
// A Fold change is atomic for each line. Each synthesized node matches its
// rendered text against all definitions at the level with schema.MatchChild.
// Matching only the intended definition could select an equal-specificity
// sibling when the rendered result is parsed again.
package parse
