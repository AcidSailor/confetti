// Package schema defines platform configuration grammars and parsed trees.
//
// Def values form a grammar of line templates, typed captures, identity,
// cardinality, relations, alternate forms, deletion rules, and merge
// strategies. A Config contains the resulting Node tree.
//
// MatchChild matches a configuration line against definitions at one tree
// level. Declaration order breaks ties between equally specific matches and
// therefore affects schema behavior. NormalizeLine collapses whitespace the way
// parsing does, so both sides agree on what a line means.
//
// MatchChildOpener lets a BlockDelim definition match a prefix, because its
// one-character delimiter is followed by block body text rather than more of
// the command. It then re-matches the opener text it would keep against every
// candidate, because a prefix match is more permissive than an anchored one and
// a node whose own text binds a different definition would be re-read as that
// other one. BlockDelim panics unless its argument is the template's last token
// and its value type matches exactly one non-space rune; the built-in "delim"
// type is such a type.
//
// Keyed siblings with the same Kind and key pair as one entity. Unkeyed,
// single-occupancy siblings pair by Kind alone, except toggle members and
// EmptyOnRemove sections. Without a Kind, pairing uses the definition.
//
// Relations target labels: a definition's Kind name or Tags. Ref, Requires,
// and ExcludeTag build the supported relations. Namespace groups keyed
// definitions into a device-wide exclusive name space, and Unique selects the
// arguments that form the name. ScopedBy narrows the space to an anchor
// instance; ScopedByDevice makes it device-wide.
// ValidateRelations checks constraints that depend on multiple definitions.
//
// Members requires a List and folds its items into canonical keyed instances.
// It excludes Key, ListDelta, ListContinues, blocks, and RespellAs on the
// membership definition.
//
// Remediation changes contain read-only references to caller-owned trees.
// CloneValue copies node values without source position or tree ownership.
//
// Invalid builder calls panic during schema construction. Mutual exclusions
// apply in both method-call orders. Validation and Diff check cross-definition
// references because schema construction has no finalization step.
package schema
