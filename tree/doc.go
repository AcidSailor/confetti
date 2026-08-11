// Package tree defines the configuration tree shared by all pipeline stages.
// Each Node stores raw text, its matched schema definition, captured fields, a
// source line, an optional raw block body, and a remediation operation. OpNone
// is the zero value so ordinary nodes have no remediation operation.
//
// A remediation Change can refer to nodes in caller-owned source trees. These
// references are read-only. Mutation helpers support transforms and folds.
// CloneValue copies value state but clears the source line.
package tree
