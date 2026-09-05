// Package ident defines node-pairing identities for parse, remediate, merge,
// and validate. Keyed siblings pair by Kind and key. Non-keyed,
// single-occupancy siblings pair by Kind when they are not toggle members or
// EmptyOnRemove sections. Key arguments use a NUL separator. Block bodies are
// excluded from identity.
//
// ScopeOf and ExclusiveName define exclusive resource scopes and names.
// Ordering defaults undeclared extents to Device to avoid missing move edges.
// Validation defaults them to PerOwner to avoid false collisions. Namespace,
// ScopedBy, and ScopedByDevice override these defaults.
package ident
