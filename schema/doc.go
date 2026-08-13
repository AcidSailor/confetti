// Package schema defines platform configuration grammars and the
// configuration tree instances they parse into.
//
// A grammar is a tree of Def line templates with typed captures and metadata
// for identity, cardinality, relationships, alternate forms, deletion rules,
// and merge resolution. A Config holds Node instances: each Node stores raw
// text, its matched Def, captured fields, a source line, an optional raw
// block body, and a remediation operation. OpNone is the zero value so
// ordinary nodes have no remediation operation.
//
// MatchChild matches a configuration line against definitions at one tree
// level. Declaration order breaks ties between equally specific matches and
// therefore affects schema behavior.
//
// Relations target labels: a definition's Kind name or Tags. Ref, Requires,
// and ExcludeTag build the supported relations. ValidateRelations checks
// constraints that depend on multiple definitions.
//
// A remediation Change can refer to nodes in caller-owned source trees. These
// references are read-only. Mutation helpers support transforms and folds.
// CloneValue copies value state but clears the source line.
//
// Invalid builder calls panic during schema construction. Mutual exclusions
// apply in both method-call orders. Validation and Diff check cross-definition
// references because schema construction has no finalization step.
package schema
