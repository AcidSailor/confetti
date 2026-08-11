// Package remediate computes the ordered CLI commands that change running
// configuration into intended configuration.
// Diff pairs the two trees by schema identity (internal/ident; Kind-aware,
// block bodies excluded), collects op-tagged changes, derives dependency
// edges (refs, exclusive resources, Requires), topologically schedules
// them. Creates ascend by declaration rank, and removes descend. Scheduling can
// split sections. The result is a new *tree.Config that render can convert to
// CLI. Result.Changes is a flat log with read-only references to source nodes.
//
// Diff is direction-independent: rollback is Diff(intended, running). The
// pipeline must not assign device-specific meaning to either argument.
// Additions emit as-is; removals emit their negation
// (def==nil text leaves); idempotent value changes emit the forward line;
// declared toggle pairs flip; Protected nodes refuse deletion with an
// Error in both policies. Diff returns no artifact when it reports an Error.
package remediate
