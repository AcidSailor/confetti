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
				// A literal terminator owns its line, so its body starts below.
				// A BlockUntil node keeps a non-nil empty body so an empty block
				// differs from a non-block node; an empty BlockDelim body is
				// dropped instead.
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
			// A delimited block that never closed would render a terminator the
			// input never had, and that text reads back as a different block.
			// Say so, because the same sentence otherwise covers a BlockUntil
			// block that is kept.
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

// closeBlock finishes a block node with the body captured for it: it reports
// any text after the closing delimiter and drops a delimited block that has no
// body. It returns the node, or nil when the drop detached it, so both callers
// store one value back over the node instead of repeating the stack repair.
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

// dropEmptyBlock removes a delimited block whose body is empty and reports
// whether it removed the node. A device accepts the empty form and then omits it
// from the running configuration, so keeping the node would invent a command the
// device does not report. The drop is reported, because a caller cannot
// otherwise tell it from input that never carried the block at all.
func dropEmptyBlock(d *diag.Diagnostics, lineNo int, n *schema.Node) bool {
	if n.Def.Block.Kind != schema.BlockDelim || !schema.EmptyDelimBody(n) {
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

// blockTail reports text after a closing delimiter and drops it. A device ends
// the block at that delimiter, so the remainder could never have come from a
// running configuration and no reading of it is safe to guess at. A
// whitespace-only remainder is dropped without a diagnostic, because it cannot
// change how a device reads the line.
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
