// Package ident defines node-pairing identities for parse, remediate, merge,
// and validate. Keyed siblings pair by Kind and key. Non-keyed,
// single-occupancy siblings pair by Kind when they are not toggle members or
// EmptyOnRemove sections. Key arguments use a NUL separator. Block bodies are
// excluded from identity.
//
// ScopeOf and ExclusiveName answer, once for every consumer, which name space
// a node's exclusive name lives in and what that name is. An undeclared extent
// is resolved with the caller's default, and the two callers pass opposite
// ones on purpose. Ordering passes Device because a needless
// release-before-claim edge is harmless while a missing one emits a plan the
// device rejects. Validation passes PerOwner because rejecting a valid
// configuration is worse than missing a collision. Namespace, ScopedBy, or
// ScopedByDevice removes the guess, and both callers then agree exactly.
package ident
