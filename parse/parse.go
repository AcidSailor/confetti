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
			blk.body = append(blk.body, st.body)
		case stepBlockEnd:
			if st.closed {
				blk.body = append(blk.body, st.body)
			}
			blockTail(d, st, blk.node)
			blk.node.Block = blk.body
			dropEmptyBlock(blk.node)
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
			// A non-nil body distinguishes an empty block from a non-block node.
			switch {
			case st.closed:
				tn.Block = []string{st.body}
				blockTail(d, st, tn)
				if dropEmptyBlock(tn) {
					tn = nil
				}
			case st.opensBlock:
				// A literal terminator owns its line, so its body starts below.
				body := []string{}
				if st.def.Block.Kind == schema.BlockDelim {
					body = []string{st.body}
				}
				blk = &blockCapture{node: tn, body: body}
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
		// A delimited block that never closed would render a terminator the
		// input never had, and that text reads back as a different block.
		if blk.node.Def.Block.Kind == schema.BlockDelim {
			blk.node.Parent.ReplaceChild(blk.node)
		}
	}

	if unknown == Drop && dropped > 0 {
		d.Add(diag.Warning, "%d nodes dropped as unsupported", dropped)
	}
	return cfg
}

// dropEmptyBlock removes a delimited block whose body is empty. A device accepts
// the empty form and then omits it from the running configuration, so keeping the
// node would invent a command the device does not report.
func dropEmptyBlock(n *schema.Node) bool {
	if n.Def.Block.Kind != schema.BlockDelim ||
		strings.Join(n.Block, "\n") != "" {
		return false
	}
	n.Parent.ReplaceChild(n)
	return true
}

// blockTail reports text after a closing delimiter. A device ends the block at
// that delimiter, so the remainder could never have come from a running
// configuration and no reading of it is safe to guess at.
func blockTail(d *diag.Diagnostics, st step, n *schema.Node) {
	if strings.TrimSpace(st.tail) == "" {
		return
	}
	d.AddAt(
		st.lineNo,
		diag.Error,
		"%s: %q follows the closing delimiter %q",
		n.Path(),
		strings.TrimSpace(st.tail),
		n.Def.Block.Term(n.Fields),
	)
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
