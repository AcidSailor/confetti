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
			// The block node is the top of the stack; storing the result there
			// keeps a deeper line from attaching to a node closeBlock detached.
			nodes[len(nodes)-1] = closeBlock(d, st, blk.node, blk.body)
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
			switch {
			case st.closed:
				tn = closeBlock(d, st, tn, []string{st.body})
			case st.opensBlock:
				// BlockUntil starts below the opener; a non-nil empty body
				// distinguishes it from an ordinary node. BlockDelim starts
				// on the opener line and drops empty bodies at close.
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
		msg := "%s: block not terminated before end of input"
		args := []any{blk.node.Path()}
		if blk.node.Def.Block.Kind == schema.BlockDelim {
			// Drop the block to avoid rendering a terminator absent from the input.
			// The diagnostic distinguishes this from BlockUntil, which is kept.
			msg += "; the command and its %d captured lines were dropped"
			args = append(args, len(blk.body))
			blk.node.Parent.ReplaceChild(blk.node)
		} else {
			blk.node.Block = blk.body
		}
		d.AddAt(blk.node.Line, diag.Error, msg, args...)
	}

	if unknown == Drop && dropped > 0 {
		d.Add(diag.Warning, "%d nodes dropped as unsupported", dropped)
	}
	return cfg
}

// closeBlock stores the body, reports trailing text, and drops empty delimited
// blocks. It returns the node or nil if dropped; callers must update the stack.
func closeBlock(
	d *diag.Diagnostics,
	st step,
	n *schema.Node,
	body []string,
) *schema.Node {
	n.Block = body
	blockTail(d, st, n)
	if dropEmptyBlock(d, st.lineNo, n) {
		return nil
	}
	return n
}

// dropEmptyBlock warns and removes an empty delimited block, matching the
// observed device behavior documented in docs/fixtures.md. It reports whether
// the node was removed.
func dropEmptyBlock(d *diag.Diagnostics, lineNo int, n *schema.Node) bool {
	if n.Def.Block.Kind != schema.BlockDelim || !n.EmptyDelimBody() {
		return false
	}
	d.AddAt(
		lineNo,
		diag.Warning,
		"%s: empty delimited block dropped; a device omits it from its running configuration",
		n.Path(),
	)
	n.Parent.ReplaceChild(n)
	return true
}

// blockTail reports discarded text after the closing delimiter. Whitespace is
// ignored; other text is an Error and is not parsed as a separate command.
func blockTail(d *diag.Diagnostics, st step, n *schema.Node) {
	if strings.TrimSpace(st.tail) == "" {
		return
	}
	d.AddAt(
		st.lineNo,
		diag.Error,
		"%s: %q follows the closing delimiter %q and was dropped",
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
