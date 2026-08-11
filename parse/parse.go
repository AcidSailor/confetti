package parse

import (
	"slices"
	"strings"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// blockCapture accumulates raw lines for an open block node until its terminator.
type blockCapture struct {
	node *tree.Node
	body []string
}

// Parse builds a tree.Config from already-text-transformed config text.
func Parse(
	s *schema.Schema,
	text string,
	policy diag.Policy,
	d *diag.Diagnostics,
) *tree.Config {
	cfg := tree.NewConfig(s)
	sc := newScanner(s)
	// nodes parallels the scanner stack; a nil entry marks an unknown frame.
	nodes := []*tree.Node{cfg.Root}
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
			if policy.Strict {
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
			tn := liveParent(nodes[:st.depth-1]).AddChild(tree.NewNode(st.txt))
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
		d.AddAt(
			blk.node.Line,
			policy.Severity(),
			"%s: block not terminated before end of input",
			blk.node.Path(),
		)
		blk.node.Block = blk.body
	}

	if !policy.Strict && dropped > 0 {
		d.Add(diag.Warning, "%d nodes dropped as unsupported", dropped)
	}
	return cfg
}

// liveParent returns the nearest stack node that is not an unknown frame.
func liveParent(nodes []*tree.Node) *tree.Node {
	for _, n := range slices.Backward(nodes) {
		if n != nil {
			return n
		}
	}
	panic("parse: no live frame on the stack (root frame must always be live)")
}
