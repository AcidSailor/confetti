// Package value defines the registry for typed captures such as
// "{{ id:vlan }}". Each type has a match pattern and an optional Check function.
// The built-in types are word, rest, and uint. Word is the default for untyped
// captures. Schemas must register platform-specific types.
//
// Register embeds a pattern in a larger expression, so it rejects anchors and
// capturing groups: an anchored pattern compiles but then matches nothing, and
// the schema layer addresses captures by name. Use the (?:...) form for groups.
package value
