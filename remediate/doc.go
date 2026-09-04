// Package remediate computes the ordered CLI commands that change running
// configuration into intended configuration.
// Diff pairs nodes by schema identity, including Kind and excluding block
// bodies. It collects operations, derives dependency edges, and schedules them
// topologically. Creates ascend by declaration rank; removals descend.
// Scheduling can split sections. Result.Tree is a new *schema.Config, and
// Result.Changes contains read-only references to source nodes.
//
// Diff is direction-independent: rollback is Diff(intended, running). The
// pipeline must not assign device-specific meaning to either argument.
// Additions emit as-is; removals emit definition-free negated leaves;
// idempotent value changes emit the forward line;
// declared toggle pairs flip; Protected nodes always reject deletion. Options
// carries the cycle policy and an optional baseline. Cycle affects only
// ordering cycles: Abort reports an Error and emits nothing; Break drops the
// greatest-keyed edge and reports a Warning. Baseline labels satisfy Requires
// without producing operations.
package remediate
