// Package graph defines the remediation-ordering dependency graph passed to
// schema OrderHooks. It is a leaf package because importing schema// would cause an import cycle. Ops contain public operation data; remediate
// stores internal operation state in a parallel slice indexed by Op.Index.
package graph
