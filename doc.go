// Package confetti is a schema-aware, offline engine for network-device CLI
// configurations. It parses, validates, canonicalizes, remediates, rolls back,
// compares, and merges configuration text without connecting to a device.
//
// An Engine owns a platform grammar, import and remediation options, and the
// import and export transforms. Merge options apply to one call. Platform
// schemas live outside this module; internal/fixture contains test fixtures and
// authoring references. See docs/design.md for the architecture and invariants
// and docs/fixtures.md for fixture grammar lineage.
//
// Remediate and Rollback run a commit check on their goal. Render, Merge, and
// Compare do not because callers can use them with incomplete configurations.
//
// WithCommitChecks registers whole-tree validators after the built-in check
// for CommitCheck, Remediate, and Rollback. A validator reports diagnostics
// and must not modify the tree.
//
// WithBaseline declares device-provided objects absent from printed
// configuration. They satisfy relations, reserve exclusive names, and cannot
// be negated. New applies all import transforms and panics on any baseline
// diagnostic, including warnings. Each validator receives a non-nil baseline
// copy. Baseline nodes never render, merge, or enter a remediation plan.
//
// Remediate and Rollback both take running, intended. Rollback restores
// canonical parsed running content, so it cannot recover original formatting
// or commands dropped during import.
package confetti
