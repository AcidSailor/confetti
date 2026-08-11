// Package transform defines text rules and tree transforms. Import text rules
// run before parsing, and export text rules run after rendering. Tree transforms
// run after parsing or before rendering.
//
// A text rule maps one line to one line, so every rule preserves the line
// count and diagnostic lines always refer to the input file. Rules that would
// remove a line blank it instead.
package transform
