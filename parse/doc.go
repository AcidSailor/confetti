// Package parse converts configuration text to a schema.Config. It uses an
// indentation stack and schema.MatchChild. An unknown line creates an isolated
// stack frame for its children. Fold canonicalizes alternate spellings after
// parsing. At each level, it applies RespellAs, list continuations, and
// membership expansion in that order.
//
// Raw block bodies, including blank lines, are preserved exactly from the
// opener through the terminator. BlockSpans uses the same indentation rules
// without building a tree so text transforms can exclude raw blocks.
//
// A BlockDelim opener that ends with its own terminator closes on that line
// with an empty body. Trailing terminators are cut while the shorter text
// still binds the same definition and delimiter, so the node re-parses
// unchanged from its rendered multi-line form. The one-line body stays in the
// opener's trailing capture, so it is normalized rather than byte-exact, and
// a definition without such a capture cannot close on one line.
//
// A Fold change is atomic for each line. Each synthesized node matches its
// rendered text against all definitions at the level with schema.MatchChild.
// Matching only the intended definition could select an equal-specificity
// sibling when the rendered result is parsed again.
package parse
