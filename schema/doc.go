// Package schema defines platform configuration grammars.
//
// A grammar is a tree of line templates with typed captures and metadata for
// identity, cardinality, relationships, alternate forms, and deletion rules.
//
// MatchChild matches a configuration line against definitions at one tree
// level. Declaration order breaks ties between equally specific matches and
// therefore affects schema behavior.
//
// Relations target labels: a definition's Kind name or Tags. Ref, Requires,
// and ExcludeTag build the supported relations. ValidateRelations checks
// constraints that depend on multiple definitions.
//
// Invalid builder calls panic during schema construction. Mutual exclusions
// apply in both method-call orders. Validation and Diff check cross-definition
// references because schema construction has no finalization step.
package schema
