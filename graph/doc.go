// Package graph defines the remediation dependency graph passed to schema
// OrderHooks. It is a leaf package because it cannot import schema. Ops contain
// public operation data; remediate stores internal state by Op.Index.
package graph
