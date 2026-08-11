// Package compare renders a remediation change log in a git-diff-style format.
// It preserves tree indentation, prefixes context with "  ", and prefixes
// changes with "+ " or "- ". It shows configuration state and omits section-exit
// tokens. Use package render on the remediation tree to produce executable CLI.
package compare
