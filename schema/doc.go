// Package schema defines platform configuration grammars and parsed trees.
//
// Def values form a grammar of line templates, typed captures, identity,
// cardinality, relations, alternate forms, deletion rules, and merge
// strategies. A Config contains the resulting Node tree.
//
// MatchChild matches a configuration line against definitions at one tree
// level. Declaration order breaks ties between equally specific matches and
// therefore affects schema behavior.
//
// Relations target labels: a definition's Kind name or Tags. Ref, Requires,
// and ExcludeTag build the supported relations. Unique and Namespace scope
// the exclusive resource a keyed definition holds, and ScopedBy or
// ScopedByDevice declares how far that name reaches. ValidateRelations checks
// constraints that depend on multiple definitions.
//
// Remediation changes contain read-only references to caller-owned trees.
// CloneValue copies node values without source position or tree ownership.
//
// Invalid builder calls panic during schema construction. Mutual exclusions
// apply in both method-call orders. Validation and Diff check cross-definition
// references because schema construction has no finalization step.
package schema
