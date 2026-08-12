// Package ident defines node-pairing identities for parse, remediate, merge,
// and validate. Keyed siblings pair by Kind and key. Non-keyed,
// single-occupancy siblings pair by Kind when they are not toggle members or
// EmptyOnRemove sections. Key arguments use a NUL separator. Block bodies are
// excluded from identity.
package ident
