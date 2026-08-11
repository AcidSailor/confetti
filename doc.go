// Package confetti is a schema-aware, offline engine for network-device CLI
// configurations. It parses, validates, canonicalizes, remediates, rolls back,
// compares, and merges configuration text without connecting to a device.
//
// The Engine ties a platform grammar (schema.Schema), a strict/lenient
// policy (diag.Policy), and the import/export transform pipelines. Grammar
// is data: platforms are authored as schemas and live outside this module
// (the in-repo internal/fixture schemas are test fixtures and authoring
// references). See docs/design.md for the architecture and the design
// invariants and docs/fixtures.md for fixture grammar lineage.
//
// Remediate and Rollback run a commit check on their goal. Render, Merge, and
// Compare do not because callers can use them with incomplete configurations.
//
// WithCommitChecks registers whole-tree validators after the built-in check
// for CommitCheck, Remediate, and Rollback.
package confetti
