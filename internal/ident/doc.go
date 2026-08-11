// Package ident defines the node-pairing identity shared by parse, remediate,
// merge, and validate. Keyed nodes with a declared Kind pair by Kind and key
// across sibling templates. Key arguments join with a NUL separator. Block
// bodies are excluded so a body-only change becomes a Modify.
package ident
