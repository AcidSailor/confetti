package parse

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
)

// Unknown selects whether an unsupported command reports an Error or Warning; Parse always drops its node.
type Unknown int

const (
	Reject Unknown = iota // Report an Error.
	Drop                  // Report a Warning and count it in the summary.
)

// blockCapture accumulates raw lines for an open block node until its terminator.
type blockCapture struct {
	node *schema.Node
	body []string
}

// Parse builds a schema.Config from text after any import text transforms.
func Parse(
	s *schema.Schema,
	text string,
	unknown Unknown,
	d *diag.Diagnostics,
) *schema.Config {
	cfg := schema.NewConfig(s)
	sc := newScanner(s)
	// nodes parallels the scanner stack; a nil entry marks an unknown frame.
	nodes := []*schema.Node{cfg.Root}
	dropped := 0
	var blk *blockCapture

	for line := range strings.SplitSeq(text, "\n") {
		st := sc.line(line)
		switch st.kind {
		case stepBlank:
		case stepBody:
			blk.body = append(blk.body, line)
		case stepBlockEnd:
			blk.node.Block = blk.body
			blk = nil
		case stepUnknown:
			if unknown == Reject {
				d.AddAt(st.lineNo, diag.Error, "unknown command: %q", st.txt)
			} else {
				d.AddAt(
					st.lineNo,
					diag.Warning,
					"unsupported command dropped: %q",
					st.txt,
				)
				dropped++
			}
			nodes = append(nodes[:st.depth-1], nil)
		case stepMatched:
			tn := liveParent(
				nodes[:st.depth-1],
			).AddChild(schema.NewNode(st.txt))
			tn.Def, tn.Fields, tn.RealIndent = st.def, st.fields, st.indent
			tn.Line = st.lineNo
			if st.opensBlock {
				// Use a non-nil empty body to distinguish an empty block from a non-block node.
				blk = &blockCapture{node: tn, body: []string{}}
			}
			nodes = append(nodes[:st.depth-1], tn)
		}
	}

	if sc.inBlock() {
		// Remove the synthetic final empty item that SplitSeq yields for a trailing newline.
		if n := len(blk.body); n > 0 && blk.body[n-1] == "" &&
			strings.HasSuffix(text, "\n") {
			blk.body = blk.body[:n-1]
		}
		// Unterminated blocks consume the remaining input and always report an Error.
		d.AddAt(
			blk.node.Line,
			diag.Error,
			"%s: block not terminated before end of input",
			blk.node.Path(),
		)
		blk.node.Block = blk.body
	}

	if unknown == Drop && dropped > 0 {
		d.Add(diag.Warning, "%d nodes dropped as unsupported", dropped)
	}
	return cfg
}

// liveParent returns the nearest stack node that is not an unknown frame.
func liveParent(nodes []*schema.Node) *schema.Node {
	for _, n := range slices.Backward(nodes) {
		if n != nil {
			return n
		}
	}
	panic("parse: no live frame on the stack (root frame must always be live)")
}
