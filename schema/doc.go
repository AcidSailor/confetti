// Package schema declares a platform grammar as a tree of node definitions,
// each a line template with typed captures ("vlan {{ id:vlan }}"), plus the
// metadata used by other packages: cardinality, Kind/Key identity,
// cross-references (Ref), prerequisite Kinds (Requires), negation and reset
// forms (NegateAs/NegateDefault/NegateFunc), raw-block capture
// (BlockDelim/BlockUntil), toggle groups (Toggles), list-valued args
// (List/ListDelta/ListKeywords/ListContinues), dual-form spellings
// (Members/RespellAs), and Protected deletion constraints.
//
// MatchChild resolves a configuration line against candidate definitions at
// one level. Declaration order resolves equal-specificity matches and is
// therefore part of the schema behavior.
//
// Builder misuse panics during schema construction. Mutual exclusions apply in
// both method-call orders. Cross-definition checks, such as Ref or Requires
// target existence, run during validation or Diff because schema construction
// has no finalize step.
package schema
