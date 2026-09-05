// Package value defines the registry for typed captures such as
// "{{ id:vlan }}". Each type has a match pattern and an optional Check function.
// The built-in types are word, rest, and uint. Word is the default for untyped
// captures. Schemas must register platform-specific types.
//
// Schema templates embed type patterns in larger expressions. Register rejects
// anchors, which restrict where embedded patterns can match, and capturing
// groups, which interfere with named schema captures. Use (?:...) for groups.
package value
