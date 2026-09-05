// Package graph defines the remediation dependency graph passed to schema
// OrderHooks. It imports only the standard library to avoid a cycle through
// schema. Ops contain public operation data; remediate stores internal state
// by Op.Index.
package graph
