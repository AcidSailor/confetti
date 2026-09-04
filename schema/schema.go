package schema

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/acidsailor/confetti/graph"
	"github.com/acidsailor/confetti/internal/listval"
	"github.com/acidsailor/confetti/value"
)

// Cardinality describes how many times a node may appear at its level.
type Cardinality int

const (
	// ZeroToOne allows at most one instance at its level (the default).
	ZeroToOne Cardinality = iota
	// ZeroToN allows any number of instances at its level.
	ZeroToN
	// One requires exactly one instance at its level.
	One
)

// Scope selects which nodes a Relation searches.
type Scope int

const (
	// ScopeTree searches the whole assembled tree.
	ScopeTree Scope = iota
	// ScopeSiblings searches the direct children of the node's parent.
	ScopeSiblings
)

// Polarity selects whether a Relation target must exist or must not.
type Polarity int

const (
	// Present requires at least one matching node.
	Present Polarity = iota
	// Absent forbids any matching node.
	Absent
)

// Relation constrains this node against nodes with Label.
type Relation struct {
	Label     string   // The label to look for.
	FromArg   string   // "" checks presence only; otherwise this capture must equal TargetKey.
	TargetKey string   // The key argument matched against FromArg's value.
	Scope     Scope    // Where to search.
	Want      Polarity // Whether a match satisfies or violates the relation.
}

// NegateKind selects how a command line is negated.
type NegateKind int

const (
	NegNoPrefix NegateKind = iota // Add or remove the "no " prefix.
	NegDefault                    // Add the "default " prefix.
	NegTemplate                   // Interpolate Template with captured fields.
	NegFunc                       // Call Func for forms a template cannot express.
)

// NegateStrategy defines how removal converts captured fields and rendered text into a negated line.
type NegateStrategy struct {
	Kind     NegateKind
	Template string
	Func     func(fields map[string]string, rendered string) string
}

// MergeKind identifies a schema-declared conflict strategy.
type MergeKind int

const (
	MergeDefault   MergeKind = iota // Declared unions lists, merges sections, and otherwise keeps the later value.
	MergeKeepFirst                  // Keep the earlier value or section.
	MergeKeepLast                   // Keep the later value or section.
	MergeCustom                     // Call Func.
)

// Outcome reports how a merge resolution settled a contested slot.
type Outcome int

const (
	Refused    Outcome = iota // The earlier value is kept and an Error is reported.
	Overridden                // The returned node won and the other value was discarded.
	Combined                  // The returned node wins and both sides' children merge.
)

// MergeStrategy defines how merge resolves this slot when two parts claim it.
type MergeStrategy struct {
	Kind MergeKind
	Func func(earlier, later *Node) (*Node, Outcome) // Set by MergeFunc.
}

// BlockKind selects how a node captures a raw multi-line block.
type BlockKind int

const (
	BlockNone  BlockKind = iota // ordinary node (zero value)
	BlockDelim                  // terminator = the value of a named capture arg
	BlockUntil                  // terminator = a fixed literal line
)

// BlockStrategy defines raw multi-line capture from an opener through a terminator.
type BlockStrategy struct {
	Kind       BlockKind
	Arg        string // BlockDelim: capture arg holding the delimiter token
	Terminator string // BlockUntil: literal line that closes the block
}

// Term returns the terminator for the captured block fields.
func (b BlockStrategy) Term(fields map[string]string) string {
	if b.Kind == BlockDelim {
		return fields[b.Arg]
	}
	return b.Terminator
}

// ListStrategy defines an unordered typed set, separator, incremental templates, optional keywords, and their domain.
type ListStrategy struct {
	Arg, Elem           string
	AddTmpl, RemoveTmpl string
	Sep                 string
	NoneWord, AllWord   string
	ExceptWord, Domain  string
}

// Keywords returns the list keyword declarations in codec form.
func (l ListStrategy) Keywords() listval.Keywords {
	return listval.Keywords{
		None:   l.NoneWord,
		All:    l.AllWord,
		Except: l.ExceptWord,
		Domain: l.Domain,
	}
}

// Schema is a grammar of the config tree.
type Schema struct {
	Registry   *value.Registry
	Roots      []*Def
	OrderHooks []func(*graph.Graph)
	// NegationWord is the schema-wide negation word ("no" unless overridden by Negation).
	NegationWord string
}

// New creates a Schema with a fresh value registry.
func New() *Schema {
	return &Schema{Registry: value.NewRegistry(), NegationWord: "no"}
}

// Node adds a new root definition with the given template and returns it.
func (s *Schema) Node(tmpl string) *Def {
	n := s.newNode(tmpl)
	s.Roots = append(s.Roots, n)
	return n
}

// OrderHook registers a function that can modify derived remediation edges before scheduling.
func (s *Schema) OrderHook(fn func(*graph.Graph)) *Schema {
	s.OrderHooks = append(s.OrderHooks, fn)
	return s
}

// Negation sets the non-empty schema-wide negation word used by the default strategy.
func (s *Schema) Negation(word string) *Schema {
	if word == "" {
		panic("schema: Negation word must be non-empty")
	}
	s.NegationWord = word
	return s
}

func (s *Schema) newNode(tmpl string) *Def {
	spec, err := compileSpec(tmpl, s.Registry)
	if err != nil {
		panic(err) // Reject invalid schema definitions during construction.
	}
	return &Def{Schema: s, spec: spec, Template: tmpl}
}

// Def describes one command line and its nested grammar.
type Def struct {
	Schema           *Schema
	Template         string // The original template used in diagnostics.
	Cardinality      Cardinality
	KindName         string
	KeyArgs          []string
	Children         []*Def
	Relations        []Relation
	TagNames         []string // Non-identity labels; see Tag.
	UniqueArgs       []string
	NamespaceLabel   string // The label that scopes the exclusive resource; see Namespace.
	Idempotent       bool
	Negate           NegateStrategy
	Merge            MergeStrategy
	SectionExitToken string // The token emitted when this section closes.
	Block            BlockStrategy
	ListSpec         ListStrategy
	ListContinuation *Def         // The base list slot that receives these items.
	MembersKind      string       // The Kind enumerated by this list line.
	Respell          *RespellSpec // The alternate-spelling rewrite.
	ToggleGroup      []*Def       // All members of the declared toggle group.
	Protected        bool
	EmptyOnRemove    bool // Removal negates children and retains the header.

	spec        *matchSpec
	continuedBy bool // Whether a continuation folds into this slot.
	toggleCanon *Def // The canonical toggle member.

	// matchOrder caches MatchChild ordering when this node leads the candidate slice.
	matchOrder atomic.Pointer[matchOrderCache]
}

// mustAllowChildren panics when a declaration on this node already excludes child nodes.
func (n *Def) mustAllowChildren() {
	if n.Block.Kind != BlockNone {
		panic("schema: block node cannot have children: " + n.Template)
	}
	if n.ListSpec.Arg != "" {
		panic("schema: list node cannot have children: " + n.Template)
	}
	if n.Respell != nil {
		panic("schema: RespellAs node cannot have children: " + n.Template)
	}
}

// mustArg panics unless arg is a capture arg of this node's template.
func (n *Def) mustArg(setter, arg string) {
	if n.spec.ArgType(arg) == "" {
		panic(
			"schema: " + setter + " arg " + arg + " is not a capture arg of " + n.Template,
		)
	}
}

// mustNonEmptyArg panics unless arg is a capture arg whose type cannot match the empty string.
func (n *Def) mustNonEmptyArg(setter, arg string) {
	n.mustArg(setter, arg)
	if n.spec.emptyArgs[arg] {
		panic(
			"schema: " + setter + " arg " + arg + " has a type whose pattern matches" +
				" the empty string: " + n.Template,
		)
	}
}

// mustTemplateArgs returns the placeholder names of tmpl and panics on any that this node does not capture.
func (n *Def) mustTemplateArgs(setter, tmpl string) []string {
	refs := templateRefs(tmpl)
	for _, arg := range refs {
		if n.spec.ArgType(arg) == "" {
			panic("schema: " + setter + " template references " + arg +
				", not a capture arg of " + n.Template)
		}
	}
	return refs
}

// mustNegatable panics when EmptyOnRemove already excludes an explicit negation strategy.
func (n *Def) mustNegatable() {
	if n.EmptyOnRemove {
		panic(
			"schema: ClearOnRemove and NegateAs/NegateDefault/NegateFunc are mutually exclusive: " + n.Template,
		)
	}
}

// Child adds a child node with the given template and returns it.
func (n *Def) Child(tmpl string) *Def {
	n.mustAllowChildren()
	c := n.Schema.newNode(tmpl)
	n.Children = append(n.Children, c)
	return c
}

// Adopt appends existing nodes as shared child grammar.
func (n *Def) Adopt(children ...*Def) *Def {
	n.mustAllowChildren()
	for _, child := range children {
		if child == nil {
			panic("schema: cannot adopt a nil child: " + n.Template)
		}
		if child.Schema != n.Schema {
			panic(
				"schema: cannot adopt a child from another schema: " + n.Template,
			)
		}
	}
	n.Children = append(n.Children, children...)
	return n
}

// Card sets node cardinality and rejects toggle members, which must remain non-keyed ZeroToOne nodes.
func (n *Def) Card(c Cardinality) *Def {
	if n.ToggleGroup != nil {
		panic(
			"schema: toggle member cardinality is fixed at ZeroToOne: " + n.Template,
		)
	}
	n.Cardinality = c
	return n
}

// Kind sets the name used for Ref resolution and pairs keyed siblings by Kind and key or unkeyed single-occupancy siblings by Kind unless they toggle or use EmptyOnRemove.
func (n *Def) Kind(k string) *Def { n.KindName = k; return n }

// Key sets the arg names that form the identity key for this node.
func (n *Def) Key(args ...string) *Def {
	seen := make(map[string]bool, len(args))
	for _, arg := range args {
		// Empty key values collide with Kind-only slot identities.
		n.mustNonEmptyArg("Key", arg)
		if seen[arg] {
			panic("schema: duplicate Key arg " + arg + ": " + n.Template)
		}
		seen[arg] = true
	}
	if n.ListSpec.Arg != "" && slices.Contains(args, n.ListSpec.Arg) {
		panic(
			"schema: List arg " + n.ListSpec.Arg + " may not be a key arg: " + n.Template,
		)
	}
	if n.MembersKind != "" {
		panic("schema: membership node may not be keyed: " + n.Template)
	}
	if n.ListContinuation != nil {
		panic("schema: continuation node may not be keyed: " + n.Template)
	}
	if n.continuedBy {
		panic("schema: ListContinues base may not be keyed: " + n.Template)
	}
	if n.ToggleGroup != nil {
		panic("schema: toggle member may not be keyed: " + n.Template)
	}
	n.KeyArgs = args
	return n
}

// Idempotent marks this node as idempotent (re-applying has no effect).
func (n *Def) MarkIdempotent() *Def { n.Idempotent = true; return n }

// Protect marks a definition as undeletable and is mutually exclusive with ClearOnRemove.
func (n *Def) Protect() *Def {
	if n.EmptyOnRemove {
		panic(
			"schema: Protect and ClearOnRemove are mutually exclusive: " + n.Template,
		)
	}
	n.Protected = true
	return n
}

// NegateAs sets a negation template and rejects references to uncaptured arguments.
func (n *Def) NegateAs(tmpl string) *Def {
	n.mustNegatable()
	n.mustTemplateArgs("NegateAs", tmpl)
	n.Negate = NegateStrategy{Kind: NegTemplate, Template: tmpl}
	return n
}

// NegateFunc sets a non-nil negation function for forms that NegateAs cannot express.
func (n *Def) NegateFunc(
	fn func(fields map[string]string, rendered string) string,
) *Def {
	if fn == nil {
		panic("schema: NegateFunc with nil func: " + n.Template)
	}
	n.mustNegatable()
	n.Negate = NegateStrategy{Kind: NegFunc, Func: fn}
	return n
}

// NegateDefault marks this node as negated with a "default " prefix.
func (n *Def) NegateDefault() *Def {
	n.mustNegatable()
	n.Negate = NegateStrategy{Kind: NegDefault}
	return n
}

// MergeKeepFirst declares that a contested slot keeps the earlier part's value.
func (n *Def) MergeKeepFirst() *Def {
	n.setMerge(MergeStrategy{Kind: MergeKeepFirst})
	return n
}

// MergeKeepLast declares that a contested slot takes the later part's value.
func (n *Def) MergeKeepLast() *Def {
	n.setMerge(MergeStrategy{Kind: MergeKeepLast})
	return n
}

// MergeFunc sets a custom conflict resolver; merge validates its result.
func (n *Def) MergeFunc(
	fn func(earlier, later *Node) (*Node, Outcome),
) *Def {
	if fn == nil {
		panic("schema: MergeFunc with nil func: " + n.Template)
	}
	n.setMerge(MergeStrategy{Kind: MergeCustom, Func: fn})
	return n
}

const mergeTwiceInGroup = "schema: a toggle group declares a merge kind twice: "

func (n *Def) setMerge(s MergeStrategy) {
	if n.Merge.Kind != MergeDefault {
		panic("schema: merge kind set twice: " + n.Template)
	}
	// Check strategies declared before Toggles.
	for _, m := range n.ToggleGroup {
		if m != n && m.Merge.Kind != MergeDefault {
			panic(mergeTwiceInGroup + n.Template)
		}
	}
	n.Merge = s
}

// EmptyOnRemove retains an always-present section header and removes each child; it excludes explicit negation, blocks, lists, and Protected.
func (n *Def) ClearOnRemove() *Def {
	if n.Protected {
		panic(
			"schema: Protect and ClearOnRemove are mutually exclusive: " + n.Template,
		)
	}
	if n.Negate.Kind != NegNoPrefix {
		panic(
			"schema: ClearOnRemove and NegateAs/NegateDefault/NegateFunc are mutually exclusive: " + n.Template,
		)
	}
	if n.Block.Kind != BlockNone {
		panic("schema: block node cannot be ClearOnRemove: " + n.Template)
	}
	if n.ListSpec.Arg != "" {
		panic("schema: list node cannot be ClearOnRemove: " + n.Template)
	}
	n.EmptyOnRemove = true
	return n
}

// SectionExit sets the token rendered at the section header's indentation when it closes.
func (n *Def) SectionExit(
	tok string,
) *Def {
	n.SectionExitToken = tok
	return n
}

// Toggles declares mutually exclusive non-keyed ZeroToOne nodes and uses the first partner as the canonical member.
func (n *Def) Toggles(partners ...*Def) *Def {
	if len(partners) == 0 {
		panic("schema: Toggles needs at least one partner: " + n.Template)
	}
	group := append([]*Def{n}, partners...)
	seen := map[*Def]bool{}
	declared := 0
	for _, m := range group {
		if m == nil || seen[m] {
			panic(
				"schema: Toggles members must be distinct nodes: " + n.Template,
			)
		}
		seen[m] = true
		if m.Merge.Kind != MergeDefault {
			declared++
		}
		if len(m.KeyArgs) > 0 || m.Cardinality != ZeroToOne {
			panic(
				"schema: toggle side must be a non-keyed ZeroToOne node: " + m.Template,
			)
		}
		if m.ToggleGroup != nil {
			panic("schema: node is already in a toggle group: " + m.Template)
		}
	}
	// Toggle members share one merge slot and permit one strategy.
	if declared > 1 {
		panic(mergeTwiceInGroup + n.Template)
	}
	for _, m := range group {
		m.ToggleGroup = group
		m.toggleCanon = partners[0]
	}
	return n
}

// BlockDelim opens a raw block terminated by a non-empty captured argument and excludes child nodes.
func (n *Def) BlockDelim(arg string) *Def {
	// Reject empty terminators because they close at the first blank line and bypass block protection.
	n.mustNonEmptyArg("BlockDelim", arg)
	n.setBlock(BlockStrategy{Kind: BlockDelim, Arg: arg})
	return n
}

// BlockUntil opens a raw block terminated by a non-empty literal line and excludes child nodes.
func (n *Def) BlockUntil(line string) *Def {
	if line == "" {
		panic("schema: BlockUntil terminator must be non-empty: " + n.Template)
	}
	n.setBlock(BlockStrategy{Kind: BlockUntil, Terminator: line})
	return n
}

func (n *Def) setBlock(b BlockStrategy) {
	if len(n.Children) > 0 {
		panic("schema: block node cannot have children: " + n.Template)
	}
	if n.EmptyOnRemove {
		panic("schema: block node cannot be ClearOnRemove: " + n.Template)
	}
	if n.Respell != nil {
		panic("schema: block node cannot be RespellAs: " + n.Template)
	}
	if n.ListContinuation != nil || n.MembersKind != "" {
		panic("schema: fold-only list node cannot open a block: " + n.Template)
	}
	n.Block = b
}

// Ref requires fromArg to match the key in a "label.keyArg" target.
func (n *Def) Ref(fromArg, target string) *Def {
	n.mustArg("Ref", fromArg)
	label, keyf, ok := strings.Cut(target, ".")
	if !ok || label == "" || keyf == "" {
		// Reject targets that cannot resolve or can alias keyless label presence.
		panic("schema: Ref target must be \"label.keyArg\": " + target)
	}
	n.Relations = append(n.Relations, Relation{
		Label: label, FromArg: fromArg, TargetKey: keyf,
		Scope: ScopeTree, Want: Present,
	})
	return n
}

// Requires requires an instance with label while this node exists.
func (n *Def) Requires(label string) *Def {
	if label == "" {
		panic("schema: Requires label must be non-empty: " + n.Template)
	}
	n.Relations = append(
		n.Relations,
		Relation{Label: label, Scope: ScopeTree, Want: Present},
	)
	return n
}

// Tag adds non-identity labels that relations can target.
func (n *Def) Tag(names ...string) *Def {
	if len(names) == 0 {
		panic("schema: Tag needs at least one name: " + n.Template)
	}
	for _, name := range names {
		if name == "" {
			panic("schema: Tag name must be non-empty: " + n.Template)
		}
	}
	n.TagNames = append(n.TagNames, names...)
	return n
}

// ExcludeTag forbids siblings with any specified label.
func (n *Def) ExcludeTag(names ...string) *Def {
	if len(names) == 0 {
		panic("schema: ExcludeTag needs at least one name: " + n.Template)
	}
	for _, name := range names {
		if name == "" {
			panic("schema: ExcludeTag name must be non-empty: " + n.Template)
		}
		n.Relations = append(
			n.Relations,
			Relation{Label: name, Scope: ScopeSiblings, Want: Absent},
		)
	}
	return n
}

// Labels returns the Kind name and Tags that relations can match.
func (n *Def) Labels() []string {
	if n.KindName == "" {
		// Clone so a caller appending to the result cannot write into TagNames.
		return slices.Clone(n.TagNames)
	}
	return slices.Concat([]string{n.KindName}, n.TagNames)
}

// HasLabel reports whether name is this definition's Kind or one of its Tags.
func (n *Def) HasLabel(name string) bool {
	return name != "" &&
		(n.KindName == name || slices.Contains(n.TagNames, name))
}

// Unique restricts exclusive resource identity to captured key arguments so remediation frees the resource before moving it.
func (n *Def) Unique(args ...string) *Def {
	for _, arg := range args {
		n.mustArg("Unique", arg)
	}
	n.UniqueArgs = args
	return n
}

// ExclusiveArgs returns the args that identify this definition's exclusive resource: Unique args when set, else the key args.
func (n *Def) ExclusiveArgs() []string {
	if len(n.UniqueArgs) > 0 {
		return n.UniqueArgs
	}
	return n.KeyArgs
}

// Namespace scopes the exclusive resource by label instead of Kind so definitions with distinct Kinds release a shared name before claiming it.
func (n *Def) Namespace(label string) *Def {
	if label == "" {
		panic("schema: Namespace label must be non-empty: " + n.Template)
	}
	n.NamespaceLabel = label
	return n
}

// List declares a leaf capture as an idempotent unordered set with numeric ranges and elements of elemType.
func (n *Def) List(arg, elemType string) *Def {
	n.mustArg("List", arg)
	if _, ok := n.Schema.Registry.Get(elemType); !ok {
		panic(
			"schema: List element type " + elemType + " is not registered: " + n.Template,
		)
	}
	if slices.Contains(n.KeyArgs, arg) {
		panic(
			"schema: List arg " + arg + " may not be a key arg: " + n.Template,
		)
	}
	if len(n.Children) > 0 {
		panic("schema: list node cannot have children: " + n.Template)
	}
	if n.EmptyOnRemove {
		panic("schema: list node cannot be ClearOnRemove: " + n.Template)
	}
	n.ListSpec.Arg, n.ListSpec.Elem = arg, elemType
	n.Idempotent = true
	return n
}

// ListDelta declares add and remove templates that must both reference the list argument.
func (n *Def) ListDelta(addTmpl, removeTmpl string) *Def {
	if n.ListSpec.Arg == "" {
		panic("schema: ListDelta requires List: " + n.Template)
	}
	if n.MembersKind != "" {
		panic(
			"schema: Members and ListDelta are mutually exclusive: " + n.Template,
		)
	}
	if n.ListContinuation != nil && n.ListContinuation != n {
		// A self-union slot survives folding and can use delta forms; a separate continuation cannot.
		panic("schema: ListContinues and ListDelta are mutually exclusive: " +
			n.Template)
	}
	if n.Respell != nil {
		panic(
			"schema: ListDelta and RespellAs are mutually exclusive: " + n.Template,
		)
	}
	for _, tmpl := range [...]string{addTmpl, removeTmpl} {
		if !slices.Contains(templateRefs(tmpl), n.ListSpec.Arg) {
			panic("schema: ListDelta template does not reference " +
				n.ListSpec.Arg + ": " + tmpl)
		}
		// Reject unknown references because interpolation would replace them with empty text.
		n.mustTemplateArgs("ListDelta", tmpl)
	}
	n.ListSpec.AddTmpl, n.ListSpec.RemoveTmpl = addTmpl, removeTmpl
	return n
}

// ListSep sets a non-empty list separator and defaults to a comma when unset.
func (n *Def) ListSep(sep string) *Def {
	if n.ListSpec.Arg == "" {
		panic("schema: ListSep requires List: " + n.Template)
	}
	if sep == "" {
		panic("schema: ListSep separator must be non-empty: " + n.Template)
	}
	n.ListSpec.Sep = sep
	n.checkDomain()
	return n
}

// ListKeywords declares optional None, All, and Except spellings and the domain required by All and Except.
func (n *Def) ListKeywords(none, all, except, domain string) *Def {
	if n.ListSpec.Arg == "" {
		panic("schema: ListKeywords requires List: " + n.Template)
	}
	if (all != "" || except != "") && domain == "" {
		panic("schema: ListKeywords all/except require a domain: " + n.Template)
	}
	n.ListSpec.NoneWord, n.ListSpec.AllWord = none, all
	n.ListSpec.ExceptWord, n.ListSpec.Domain = except, domain
	n.checkDomain()
	return n
}

// checkDomain validates the domain after ListKeywords or ListSep changes it.
func (n *Def) checkDomain() {
	if n.ListSpec.Domain == "" {
		return
	}
	if _, err := listval.Expand(n.ListSpec.Domain, n.ListSpec.Sep); err != nil {
		panic("schema: ListKeywords domain does not expand: " + n.Template +
			": " + err.Error())
	}
}

// ListContinues unions this list line into a non-keyed sibling base slot during import and removes the continuation line.
func (n *Def) ListContinues(base *Def) *Def {
	if base == nil {
		panic("schema: ListContinues base must be a node: " + n.Template)
	}
	if n.ListSpec.Arg == "" {
		panic("schema: ListContinues requires List: " + n.Template)
	}
	if n.Block.Kind != BlockNone {
		panic("schema: block node cannot declare ListContinues: " + n.Template)
	}
	if base.ListSpec.Arg == "" {
		panic(
			"schema: ListContinues base must be a list node: " + base.Template,
		)
	}
	if base != n && base.ListContinuation != nil {
		panic(
			"schema: ListContinues base is itself a continuation: " + base.Template,
		)
	}
	if len(base.KeyArgs) > 0 {
		panic("schema: ListContinues base may not be keyed: " + base.Template)
	}
	// Self-union keeps the first instance as the slot and unions later instances into it.
	if base != n && n.ListSpec.AddTmpl != "" {
		panic("schema: ListContinues and ListDelta are mutually exclusive: " +
			n.Template)
	}
	if n.MembersKind != "" {
		panic("schema: ListContinues and Members are mutually exclusive: " +
			n.Template)
	}
	if len(n.KeyArgs) > 0 {
		panic("schema: continuation node may not be keyed: " + n.Template)
	}
	if n.Respell != nil || base.Respell != nil {
		panic(
			"schema: ListContinues and RespellAs are mutually exclusive: " + n.Template,
		)
	}
	n.ListContinuation = base
	base.continuedBy = true // Make a later base.Key call reject the invalid combination.
	return n
}

// Members folds each list item into a canonical keyed instance of Kind and excludes keys, delta forms, continuations, blocks, and RespellAs.
func (n *Def) Members(kind string) *Def {
	if kind == "" {
		panic("schema: Members kind must be non-empty: " + n.Template)
	}
	if n.ListSpec.Arg == "" {
		panic("schema: Members requires List: " + n.Template)
	}
	if n.Block.Kind != BlockNone {
		panic("schema: block node cannot declare Members: " + n.Template)
	}
	if n.ListSpec.AddTmpl != "" {
		panic(
			"schema: Members and ListDelta are mutually exclusive: " + n.Template,
		)
	}
	if len(n.KeyArgs) > 0 {
		panic("schema: membership node may not be keyed: " + n.Template)
	}
	if n.ListContinuation != nil {
		panic("schema: ListContinues and Members are mutually exclusive: " +
			n.Template)
	}
	if n.Respell != nil {
		panic(
			"schema: Members and RespellAs are mutually exclusive: " + n.Template,
		)
	}
	n.MembersKind = kind
	return n
}

// RespellSpec defines an alternate spelling that folds into an interpolated Header and child lines.
type RespellSpec struct {
	Header   string
	Children []string
}

// RespellAs folds this alternate spelling into an interpolated canonical header and children during import.
func (n *Def) RespellAs(header string, children ...string) *Def {
	if n.Respell != nil {
		panic("schema: node already declares RespellAs: " + n.Template)
	}
	if n.MembersKind != "" {
		panic(
			"schema: Members and RespellAs are mutually exclusive: " + n.Template,
		)
	}
	if n.ListSpec.AddTmpl != "" {
		panic(
			"schema: ListDelta and RespellAs are mutually exclusive: " + n.Template,
		)
	}
	if n.ListContinuation != nil || n.continuedBy {
		panic(
			"schema: ListContinues and RespellAs are mutually exclusive: " + n.Template,
		)
	}
	if n.Block.Kind != BlockNone {
		panic("schema: block node cannot be RespellAs: " + n.Template)
	}
	if len(n.Children) > 0 {
		panic("schema: RespellAs node cannot have children: " + n.Template)
	}
	headerRefs := n.mustTemplateArgs("RespellAs", header)
	for _, tmpl := range children {
		n.mustTemplateArgs("RespellAs", tmpl)
	}
	if len(headerRefs) == 0 {
		panic(
			"schema: RespellAs header references no capture arg of " + n.Template +
				": " + header,
		)
	}
	n.Respell = &RespellSpec{Header: header, Children: children}
	return n
}

// interp scans typeless {{ name }} placeholders, substitutes each through sub, and preserves text outside valid pairs.
func interp(tmpl string, sub func(name string) string) string {
	var b strings.Builder
	for {
		before, after, found := strings.Cut(tmpl, "{{")
		b.WriteString(before)
		if !found {
			break
		}
		name, rest, closed := strings.Cut(after, "}}")
		if !closed {
			b.WriteString("{{") // Preserve an unterminated opener.
			b.WriteString(after)
			break
		}
		b.WriteString(sub(strings.TrimSpace(name)))
		tmpl = rest
	}
	return b.String()
}

// Interpolate replaces typeless {{ name }} placeholders and preserves text outside valid placeholder pairs.
func Interpolate(tmpl string, fields map[string]string) string {
	return interp(tmpl, func(name string) string { return fields[name] })
}

// templateRefs returns placeholder names from a typeless interpolation template.
func templateRefs(tmpl string) []string {
	var names []string
	interp(tmpl, func(name string) string {
		names = append(names, name)
		return ""
	})
	return names
}

// TogglePartner returns the other member of a two-member group or nil for other group sizes.
func (n *Def) TogglePartner() *Def {
	if len(n.ToggleGroup) != 2 {
		return nil
	}
	for _, m := range n.ToggleGroup {
		if m != n {
			return m
		}
	}
	return nil
}

// ToggleCanonical returns the group's canonical member or the receiver when ungrouped.
func (n *Def) ToggleCanonical() *Def {
	if n.toggleCanon == nil {
		return n
	}
	return n.toggleCanon
}

// ArgType returns the value-type name declared for the given capture arg.
func (n *Def) ArgType(name string) string { return n.spec.ArgType(name) }

// MatchLine matches a line exactly and returns captured fields on success.
func (n *Def) MatchLine(
	line string,
) (map[string]string, bool) {
	return n.spec.Match(line)
}

// Render produces the config line by substituting field values into the template.
func (n *Def) Render(f map[string]string) string { return n.spec.Render(f) }

// matchOrderCache stores one candidate slice and its stable specificity order for concurrent reuse.
type matchOrderCache struct {
	src     []*Def // The candidates used to compute ordered.
	ordered []*Def
}

// MatchChild returns the first match in descending literal specificity and resolves ties by declaration order.
func MatchChild(
	candidates []*Def,
	line string,
) (*Def, map[string]string, bool) {
	if len(candidates) == 0 {
		return nil, nil, false
	}
	lead := candidates[0]
	memo := lead.matchOrder.Load()
	if memo == nil || !slices.Equal(memo.src, candidates) {
		ordered := slices.Clone(candidates)
		slices.SortStableFunc(ordered, func(a, b *Def) int {
			return b.spec.litLen - a.spec.litLen
		})
		memo = &matchOrderCache{src: candidates, ordered: ordered}
		lead.matchOrder.Store(memo)
	}
	for _, c := range memo.ordered {
		if f, ok := c.spec.Match(line); ok {
			return c, f, true
		}
	}
	return nil, nil, false
}

// BindsDef reports whether MatchChild selects want and returns its captured fields.
func BindsDef(
	candidates []*Def,
	want *Def,
	line string,
) (map[string]string, bool) {
	def, fields, ok := MatchChild(candidates, line)
	if !ok || def != want {
		return nil, false
	}
	return fields, true
}
