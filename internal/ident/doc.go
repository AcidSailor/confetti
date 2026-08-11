// Package ident defines the node-pairing identity shared by parse, remediate,
// merge, and validate. Keyed nodes with a declared Kind pair by Kind and key
// across sibling templates. A non-keyed, single-occupancy, non-toggle node
// with a Kind pairs by Kind alone, so variant spellings of one slot diff as
// one entity; toggle members keep flip semantics and EmptyOnRemove sections
// are excluded because they have no header negation to pair with. Key
// arguments join with a NUL separator. Block bodies are excluded so a
// body-only change becomes a Modify.
package ident
