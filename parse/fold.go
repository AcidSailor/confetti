package parse

import (
	"maps"

	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/internal/ident"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/tree"
)

// Fold canonicalizes RespellAs, ListContinues, and Members spellings in that order and leaves a line unchanged when it cannot fold atomically.
func Fold(cfg *tree.Config, d *diag.Diagnostics) {
	foldLevel(cfg.Root, cfg.Schema.Roots, d)
}

// levelFold carries one level's parent, sibling candidates, and diagnostics through the fold passes.
type levelFold struct {
	parent     *tree.Node
	candidates []*schema.Node
	d          *diag.Diagnostics
}

func foldLevel(
	parent *tree.Node,
	candidates []*schema.Node,
	d *diag.Diagnostics,
) {
	lf := &levelFold{parent: parent, candidates: candidates, d: d}
	// Apply RespellAs first.
	var respells []*tree.Node
	for _, c := range parent.Children {
		if def := c.Def; def != nil && def.Respell != nil {
			respells = append(respells, c)
		}
	}
	for _, m := range respells {
		lf.foldRespell(m)
	}

	// Apply list continuations second.
	var conts []*tree.Node
	for _, c := range parent.Children {
		if def := c.Def; def != nil && def.ListContinuation != nil {
			conts = append(conts, c)
		}
	}
	for _, m := range conts {
		lf.foldContinuation(m)
	}

	// Scan the complete level before membership folding so earlier membership lines can find later canonical instances.
	var members []*tree.Node
	seen := map[ident.Ident]*tree.Node{}
	for _, c := range parent.Children {
		def := c.Def
		if def == nil {
			continue
		}
		if def.MembersKind != "" {
			members = append(members, c)
			continue
		}
		if len(def.KeyArgs) > 0 {
			if id := ident.Of(c); seen[id] == nil {
				seen[id] = c
			}
		}
	}
	for _, m := range members {
		lf.foldOne(m, seen)
	}

	for _, c := range parent.Children {
		if ident.IsSection(c) {
			foldLevel(c, c.Def.Children, d)
		}
	}
}

// foldContinuation unions one continuation into its sibling base slot and creates the slot when absent.
func (lf *levelFold) foldContinuation(m *tree.Node) {
	parent, candidates, d := lf.parent, lf.candidates, lf.d
	def := m.Def
	base := def.ListContinuation
	mls, bls := def.ListSpec, base.ListSpec
	items, err := listval.Resolve(m.Fields[mls.Arg], mls.Sep, mls.Keywords())
	if err != nil {
		return // Leave the line for Phase A list validation.
	}

	var slot *tree.Node
	for _, c := range parent.Children {
		if c.Def == base && continuationFieldsMatch(m, c, mls.Arg) {
			if slot != nil && base != def {
				d.AddAt(m.Line, diag.Error,
					"%s: continuation matches multiple base slots", m.Path())
				return
			}
			slot = c
			if base == def {
				break
			}
		}
	}
	if slot == m {
		// The first self-union instance is already the base slot.
		return
	}
	if slot == nil {
		lf.synthesizeBase(m, base, items)
		return
	}

	bkw := bls.Keywords()
	cur, err := listval.Resolve(slot.Fields[bls.Arg], bls.Sep, bkw)
	if err != nil {
		// Report the continuation because Phase A reports the malformed base at a different line.
		d.AddAt(m.Line, diag.Warning,
			"%s: continuation left unfolded: base list %q is malformed",
			m.Path(), slot.Fields[bls.Arg])
		return
	}
	raw := listval.Canonical(append(cur, items...), bls.Sep, bkw)
	if raw == "" {
		d.AddAt(m.Line, diag.Error,
			"%s: empty union has no spelling (base declares no none keyword)",
			m.Path())
		return
	}
	f := maps.Clone(slot.Fields)
	f[bls.Arg] = raw
	// Match the rewritten line against all candidates and require the base definition.
	text := base.Render(f)
	mdef, fields, ok := schema.MatchChild(candidates, text)
	if !ok || mdef != base {
		d.AddAt(m.Line, diag.Error,
			"%s: cannot fold continuation: %q does not re-parse to template %q",
			m.Path(), text, base.Template)
		return
	}
	slot.Def, slot.Fields = base, fields
	// Clear the source line because the union combines multiple lines.
	slot.Line = 0
	slot.Text = text
	parent.ReplaceChild(m)
}

func continuationFieldsMatch(m, slot *tree.Node, listArg string) bool {
	slotFields := slot.Fields
	for arg, mv := range m.Fields {
		if arg == listArg {
			continue
		}
		if sv, ok := slotFields[arg]; ok && sv != mv {
			return false
		}
	}
	return true
}

// synthesizeBase converts a continuation into a matched base slot when no base exists at the level.
func (lf *levelFold) synthesizeBase(
	m *tree.Node,
	base *schema.Node,
	items []string,
) {
	parent, candidates, d := lf.parent, lf.candidates, lf.d
	bls := base.ListSpec
	raw := listval.Canonical(items, bls.Sep, bls.Keywords())
	if raw == "" {
		d.AddAt(
			m.Line,
			diag.Error,
			"%s: continuation resolves to no items",
			m.Path(),
		)
		return
	}
	f := maps.Clone(m.Fields)
	delete(f, m.Def.ListSpec.Arg)
	f[bls.Arg] = raw
	text := base.Render(f)
	def, fields, ok := schema.MatchChild(candidates, text)
	if !ok || def != base {
		d.AddAt(
			m.Line,
			diag.Error,
			"%s: cannot synthesize base slot: %q does not re-parse to template %q",
			m.Path(),
			text,
			base.Template,
		)
		return
	}
	nn := tree.NewNode(text)
	nn.Def, nn.Fields, nn.RealIndent = base, fields, m.RealIndent
	nn.Line = m.Line
	parent.ReplaceChild(m, nn)
}

// foldOne commits a membership fold only when every item synthesizes successfully.
func (lf *levelFold) foldOne(m *tree.Node, seen map[ident.Ident]*tree.Node) {
	parent, candidates, d := lf.parent, lf.candidates, lf.d
	def := m.Def
	kind := def.MembersKind
	canon, keyArg := resolveCanonical(candidates, kind, def, m.Fields)
	if canon == nil {
		d.AddAt(
			m.Line,
			diag.Error,
			"%s: membership line has no canonical %q def among its siblings"+
				" coverable from this line's fields",
			m.Path(),
			kind,
		)
		return
	}
	ls := def.ListSpec
	items, err := listval.Resolve(m.Fields[ls.Arg], ls.Sep, ls.Keywords())
	if err != nil {
		return // Leave the line for Phase A list validation.
	}
	var repl []*tree.Node
	for _, it := range items {
		// Copy non-list fields and use the item for the remaining key component.
		f := maps.Clone(m.Fields)
		delete(f, ls.Arg)
		f[keyArg] = it
		text := canon.Render(f)
		// Match the rendered line against all candidates and require the canonical definition.
		mdef, fields, ok := schema.MatchChild(candidates, text)
		if !ok || mdef != canon {
			d.AddAt(
				m.Line,
				diag.Error,
				"%s: cannot synthesize canonical %q instance from item %q:"+
					" %q does not re-parse to template %q",
				m.Path(),
				kind,
				it,
				text,
				canon.Template,
			)
			return
		}
		nn := tree.NewNode(text)
		nn.Def, nn.Fields, nn.RealIndent = canon, fields, m.RealIndent
		nn.Line = m.Line
		// Deduplicate only when the existing identity agrees on all synthesized fields; Phase A handles conflicting duplicate keys.
		if ex, ok := seen[ident.Of(nn)]; ok &&
			fieldsSubset(fields, ex.Fields) {
			continue
		}
		repl = append(repl, nn)
	}
	for _, nn := range repl {
		if id := ident.Of(nn); seen[id] == nil {
			seen[id] = nn
		}
	}
	parent.ReplaceChild(m, repl...)
}

// fieldsSubset reports whether super contains each field and value from sub.
func fieldsSubset(sub, super map[string]string) bool {
	for k, v := range sub {
		if sv, ok := super[k]; !ok || sv != v {
			return false
		}
	}
	return true
}

// resolveCanonical returns the first sibling of Kind with one key argument supplied by the membership list.
func resolveCanonical(
	candidates []*schema.Node,
	kind string,
	member *schema.Node,
	fields map[string]string,
) (*schema.Node, string) {
	// Exclude the membership list argument because foldOne removes it before rendering.
	lsArg := member.ListSpec.Arg
	for _, c := range candidates {
		if c == member || c.KindName != kind {
			continue
		}
		missing := ""
		ok := true
		for _, k := range c.KeyArgs {
			if _, have := fields[k]; have && k != lsArg {
				continue
			}
			if missing != "" {
				ok = false // Reject a definition with two uncovered key components.
				break
			}
			missing = k
		}
		if ok && missing != "" {
			return c, missing
		}
	}
	return nil, ""
}

// foldRespell atomically rewrites one alternate spelling into a matched canonical header and children.
func (lf *levelFold) foldRespell(m *tree.Node) {
	parent, candidates, d := lf.parent, lf.candidates, lf.d
	def := m.Def
	rs := def.Respell
	headerText := schema.Interpolate(rs.Header, m.Fields)
	hdef, hfields, ok := schema.MatchChild(candidates, headerText)
	// Reject chained respells because replacement does not run another pass.
	if !ok || (hdef == def) || (hdef.Respell != nil) {
		d.AddAt(m.Line, diag.Error,
			"%s: cannot respell: %q does not bind a canonical def",
			m.Path(), headerText)
		return
	}
	type bind struct {
		text   string
		cdef   *schema.Node
		fields map[string]string
	}
	binds := make([]bind, 0, len(rs.Children))
	for _, ct := range rs.Children {
		text := schema.Interpolate(ct, m.Fields)
		cdef, cfields, cok := schema.MatchChild(hdef.Children, text)
		if !cok {
			d.AddAt(m.Line, diag.Error,
				"%s: cannot respell: child %q does not bind under %q",
				m.Path(), text, hdef.Template)
			return
		}
		binds = append(binds, bind{text, cdef, cfields})
	}
	// Merge hosts by header text because the header can omit captured arguments.
	var host *tree.Node
	for _, c := range parent.Children {
		if c != m && c.Def == hdef && c.Text == headerText {
			host = c
			break
		}
	}
	if host == nil {
		host = tree.NewNode(headerText)
		host.Def, host.Fields, host.RealIndent = hdef, hfields, m.RealIndent
		host.Line = m.Line
		parent.ReplaceChild(m, host)
	} else {
		parent.ReplaceChild(m)
	}
	for _, b := range binds {
		dup := false
		for _, c := range host.Children {
			if c.Text == b.text {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		nn := tree.NewNode(b.text)
		nn.Def, nn.Fields, nn.RealIndent = b.cdef, b.fields, host.RealIndent+2
		nn.Line = m.Line
		// Preserve source order by inserting before the first child from a later nonzero source line.
		var before *tree.Node
		for _, c := range host.Children {
			if c.Line > m.Line {
				before = c
				break
			}
		}
		if before == nil {
			host.AddChild(nn)
		} else {
			host.InsertChildBefore(before, nn)
		}
	}
}
