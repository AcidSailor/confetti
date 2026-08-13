# confetti design

confetti is a schema-aware, offline engine for network-device CLI
configurations: parse, validate (Juniper-style commit check), canonicalize,
remediate (ordered CLI converging running → intended), roll back, diff, and
merge. It processes text without device connections. The core contains no
vendor-specific logic.

This document defines the current design.

## Scope

- Schemas are Go code built with the `schema` package. confetti does not
  provide an external schema language or data-file format.
- confetti processes configuration text and returns trees, diagnostics,
  rendered text, comparison views, and remediation artifacts. It does not
  connect to devices, apply artifacts, or model commits and transactions.
- confetti is a library. Callers provide platform packages, device transport,
  command-line interfaces, and exit-code policy.

## Package map

```
confetti (root)   Engine: option wiring + the pipelines below
├── schema        grammar and config tree (Def templates, kinds/keys/refs,
│                 strategies, MatchChild, Node, Config, op tags)
├── value         value-type registry (builtins: word, rest, uint)
├── parse         text → tree (indent stack, block capture, BlockSpans) and
│                 the import fold (Respell → ListContinues → Members)
├── validate      ImportCheck (values, cardinality, dup keys, toggles,
│                 required children) and CommitCheck (relations: refs,
│                 Requires, tag exclusions)
├── render        tree → canonical text
├── transform     text rules (DropLines, PerLineSub) + tree transform seam
├── remediate     Diff: pair → collect ops → derive edges → schedule →
│                 materialize; Result{Tree, Changes}
├── graph         ordering graph passed to schema.OrderHook; pure leaf
│                 package, stdlib only (see Layering)
├── compare       Changes → git-diff-style human view
├── merge         fragments → one tree, per-call conflict resolution
├── diag          two-severity diagnostics with optional source line
└── internal/
    ├── ident     node-pairing identity shared by remediate + merge
    ├── lcp       longest-common-prefix for scheduler section affinity
    ├── listval   the one comma+range list codec
    ├── testtypes domain value types for core tests
    ├── valcheck  the shared ipv4 and numeric-range value checks
    └── fixture/{alpha,beta}  imaginary-platform schemas (see fixtures.md)
```

Layering rules:

- **`graph` is a leaf package** with no confetti imports. The import graph
  enforces this rule: `schema` uses `graph` types in the `OrderHook`
  signature, so importing `schema` from `graph` would cause a cycle.
  `remediate` keeps detailed per-operation state in a parallel slice indexed
  by operation index.
- **Presentation stays out of `remediate`**: `compare` renders the change log,
  while `remediate` produces it. This matches the boundary between `render`
  and the operation-tagged tree.
- **Use one leaf package per capability and minimize edits to existing code.**
  A feature belongs in the lowest package that owns its behavior.
- Core ships no platforms. Domain value types (`ifname`, `ipv4`, `vlan`,
  `asn`) are platform data; the builtin registry is exactly the structural
  three: `word`, `rest`, and `uint`. `word` is required because it is the
  default for untyped captures. A test enforces this set.
- Prefer limited duplication to an early shared abstraction. Extract a shared
  package after three uses. Shared identity logic belongs in `internal/ident`;
  the IPv4 and range value checks reached three uses and belong in
  `internal/valcheck`.

## Pipelines

```
Import:    text transforms (outside block spans) → parse → fold
           (Respell → ListContinues → Members) → user tree transforms
           → ImportCheck
CommitCheck: relations (refs, Requires, exclusions) over an assembled tree
           → custom validators (WithCommitChecks)
Render:    user tree transforms → render → text transforms (outside blocks)
Remediate: CommitCheck(intended) + Diff(running, intended)
Rollback:  CommitCheck(running)  + Diff(intended, running)
Compare:   Diff + compare.Render; no commit check
Merge:     fold parts left-to-right; no commit check
```

The fold runs before user tree transforms so everything downstream of parse
sees canonical instances only. Fold order within a level is fixed:
Respell first (a respelled line may *be* a membership or continuation line in
canonical form), continuations before membership (they may create the slots
the membership pre-scan must see).

## Invariants

These rules define required behavior. Regression tests enforce most rules.
The explanations record constraints that tests do not show.

### Policy and safety constraints

- **Recovery options are stage-specific and do not relax constraints on
  schema-known configuration.** `parse.Unknown` handles unknown input,
  `merge.Options` resolves accepted values, and `remediate.Cycle` handles
  ordering cycles. Fold and Diff authoring errors are always Errors.
- **Parse and remediation refuse by default.** Their zero values are `Reject`
  and `Abort`. Merge defaults to `Declared`, which applies the schema strategy,
  unions lists, merges matching sections, and keeps the later value otherwise.
  `Refuse` rejects conflicts not resolved by the schema or list union.
- **`Protect()` always rejects deletion.** The check applies to the removal
  loop, `ckReplace`, removed subtree descendants, and keyed identity changes.
  `ckReplace` rejects both operations because an add without its removal could
  create duplicates.
- **The Protected boundary: value changes are not deletions.** `ckModify`,
  a toggle flip, and list deltas on a protected node stay allowed.
- **Protected is all-or-nothing for header negation but per-child for an
  `EmptyOnRemove` expansion**, and `Protected` × `EmptyOnRemove` on one def
  is an authoring panic ("never delete this" contradicts "here is how to
  delete this").
- **Safety refusals suppress output.** An `Abort` cycle produces an empty tree
  and empty Changes. A Protected refusal produces no operation or Change.
  Other Diff errors can retain a Result for inspection.
- **A merge resolver must not mutate its inputs.** It returns `earlier`,
  `later`, or a fresh parentless node. Returning an owned node would splice
  two trees together.
- **Fresh resolver nodes must preserve slot identity.** A fresh node must have
  a definition, keep the contested pairing identity, render its text from its
  fields, and bind its definition through `schema.BindsDef` at that level. A
  fresh node cannot replace a section because it has no children. Invalid
  output reports an Error and keeps the earlier value.
- **A `Combined` result must preserve both values.** Sections can combine only
  when both inputs and the result use one definition. Returning `earlier` for
  different leaf values reports a Warning because the later value is absent.
  `Refused` keeps the earlier stanza whole.

### Schema construction constraints

- **Authoring mistakes panic at schema-build time; anything requiring the
  whole schema becomes a Diff-time or derivation-time Error.** There is no
  schema finalize step, so facts across definitions, such as a `Requires`
  target's existence or whether a definition has children, cannot panic.
- **Each mutual exclusion and prerequisite must hold in both builder call
  orders.** Tests must cover `Key` after `ListContinues` and `Key` or `Card`
  after `Toggles` in both call orders.
- **Declaration order is the only ordering and tie-break signal.** There is
  no `OrderWeight` and no `AllowDup`. Equal-specificity `MatchChild`
  overlaps resolve by declaration order. Therefore, declaration order is part
  of schema behavior. Examples include the physical-port definition before
  the general definition and the canonical section before membership syntax.
- **Represent platform-specific behavior as schema data or hooks, not core code.**
  `NegateStrategy`, `BlockStrategy`, `ListStrategy`, `Toggles`,
  `OrderHook`, `WithCommitChecks`, and tree transforms keep the engine
  platform-independent.
- **Whole-tree validators belong to `Engine`.** `OrderHook` and `MergeFunc`
  are schema behavior. Validators are Engine composition, so Engines that
  share a schema can use different validator sets. `WithCommitChecks` appends
  validators after the built-in `CommitCheck`, in registration order. They run
  for `CommitCheck`, `Remediate` (intended), and `Rollback` (running). `Render`,
  `Compare`, and `Merge` skip them. A validator reports into its own `diag.Diagnostics`,
  merged after it returns, so it cannot drop what earlier checks recorded. It
  must not modify the tree: `Remediate` validates the caller's `intended`
  before `Diff`, so a mutation would silently change the remediation. A nil
  validator panics at registration.
- **Toggle pairs are declared, never text-detected.** The `"no "`-prefix
  heuristic has no fallback. A test verifies that an undeclared pair emits
  separate remove and add operations.
- **At most one member of a toggle group may declare a merge kind.** The
  group shares one merge slot, so a second declaration would make resolution
  depend on encounter order. The check holds in both builder call orders.

### Pairing and diff semantics

- **Remediation output is a new `*schema.Config` of operation-tagged nodes.**
  The standard renderer produces executable CLI, and `schema.Walk` supports
  inspection. `OpNone` is the zero value because every node has an operation.
  `OpRemove` nodes have no definition: a negated line does not match its
  positive definition, and the missing definition suppresses an incorrect
  section-exit token on a negated block.
- **Diff never mutates its inputs.** `Change.Running`/`.Intended` alias the
  caller's trees read-only; the artifact is built from new nodes.
- **`Diff` is direction-independent, and `Remediate` and `Rollback` use the
  same argument order: `running, intended`.** Change the method name, not the
  argument order, to request rollback. This remains valid only while pipeline
  logic does not assign device-specific meaning to an argument. A `Result`
  cannot be inverted because OpRemove has no definition and OpModify stores
  only the new value.
- **Pairing identity uses Kind instead of definition pointers.** Keyed sibling
  templates with the same Kind and key pair as one entity.
- **A `KindedSingle` pairs by Kind alone.** It is non-keyed,
  single-occupancy, non-toggle, not `EmptyOnRemove`, and has a Kind. Variant
  spellings therefore produce one Replace: negate the running spelling, then
  add the intended spelling. Toggle members retain flip semantics.
  `EmptyOnRemove` sections cannot pair because they have no header negation.
- **Duplicate Kind-paired spellings produce diagnostics.** Diff removes stale
  running spellings before it reissues the intended value. Multiple intended
  spellings produce an Error, and only the first is applied. `ImportCheck`
  rejects multiple spellings at one level.
- **Block bodies are not part of identity.** A body-only change is a Modify.
- **Merge gives every non-keyed ZeroToOne definition one slot per level.**
  This includes definitions without a Kind and toggle groups. Remediation
  requires a Kind because unmarked sibling definitions can be independent.
  Diff reports an Error when separate Add and Remove operations target the
  same definition or a Kind shared by toggle and non-toggle siblings,
  because the emitted pair can leave the device slot in either state.
- **A section header is never `OpModify` for identity**: changed header
  identity is Remove+Add of the whole section. A kept section whose
  children all vanish keeps its header and negates each child instead of a
  collapsed `no <section>`, which could reset too much. A kept section with
  zero operations never
  materializes. This prevents unnecessary `EmptyOnRemove` output.
- **A replace pair (negate + re-add) is one schedulable unit, adjacent by
  construction.** Its adjacency must not depend on sort-key coincidence. The
  scheduler can reorder other operations.

### Ordering

- **Ordering is derived per instance from schema semantics (`Ref`, identity
  conflicts, `Requires`, and sibling exclusions) and is topologically sorted.** Use `OrderHook`
  for platform-specific ordering. By default, creates ascend by
  declaration rank, removes descend (define-before-reference on add,
  reference-before-definition on removal).
- **Preserve declaration-rank order** when it satisfies every derived edge.
  The output must remain byte-identical to pre-graph output. Derivation and
  scheduling must not depend on map iteration order.
- **Edges connect only operations in the plan.** An unchanged reference target
  needs no edge; commit check validates its existence. Scheduler affinity and
  materializer path changes can split a section. Edge derivation defines and
  tests this behavior.
- **Declare exclusivity with `Unique`.** `Requires` is
  existential only. Use `OrderHook` for sequencing preferences that have no
  existence semantics.
- **Sibling exclusions order removals before additions.** `Diff` does not check
  intermediate states, but the emitted operations must remain valid for the
  device. Each conflicting sibling is removed before its excluder is installed
  (`no switchport` before `ip address`). Edges between a pure removal and a pure
  addition cannot cycle. `Replace` and cross-definition `Modify` operations can
  occur at either end of an edge and can form cycles.
- **Absent relations derive only exclusion edges.** They do not identify a
  required provider, so they do not derive reference or prerequisite edges.

### Import folds (dual-form, lists, respell)

- **Any code path that assigns a definition and text to a node must pass the
  rendered text to `schema.MatchChild` with all candidates at that level. The
  result must bind the intended definition.** Matching only the intended
  definition can select a different equal-specificity sibling after parsing,
  produce a different identity, and cancel remediation. Each new fold or
  synthesis path requires a regression test for this condition.
- **A fold is atomic for each line.** A partial fold could lose items.
  Membership processing scans the complete level before it changes nodes
  because a compressed line can precede matching sections.
- **Canonical form contains sections only.** Render does not reconstruct a
  compressed membership line.
  `Import∘Render` is a fixed point after one pass.
- **Spelling differences are not drift.** Range vs explicit vs keyword
  spellings of the same set must yield an empty diff; set-equal list slots
  are a no-op even when raw text differs. Keyword spellings survive on
  untouched slots. Canonicalization occurs only where a value is
  recomputed (merge union, continuation fold).
- **List semantics have four views.** `Parts` returns written items for
  validation and reference checks. `Resolve` returns semantic sets for diff,
  union, and ordering. `Canonical` returns the shortest output spelling.
  `Intervals` converts a resolved set into compact members for overlap checks.
  `internal/listval` is the only comma-and-range codec.
- **List deltas fall back to whole-line Modify when they cannot represent the
  change** because of expansion failure, non-list field differences, or
  missing templates.

### Blocks and round-trip

- **Canonical outside blocks, byte-exact inside blocks.** Block bodies are
  exact raw text: no normalization, blank lines preserved, and the
  terminator re-emitted. Global round-trip contract:
  `render(parse(x)) == canonical(x)`, and canonical is idempotent. Existing
  input is not always byte-identical after rendering. Rollback therefore
  restores canonical parsed running configuration, not original bytes. Lines
  dropped by a `parse.Drop` import cannot be restored.
- **Text transforms never run inside block spans.** Span detection is
  level-aware (`parse.BlockSpans` mirrors the parser's indent walk) and the
  guard protects the union of the raw-text walk and the
  rules-applied-everywhere walk. If the results differ, the union protects the
  span. This can retain noise for diagnostics but does not alter block content.

### Commit-check boundary

Run a commit check only on the goal and only before an operation that produces
device commands. `Remediate` checks intended, and `Rollback` checks running.
Both still return the Result for inspection when errors occur. `Render`,
`Merge`, and `Compare` do not run a commit check because they format, assemble,
or inspect configurations. Diff converges to the checked goal, so it does not
check references in running configuration that will be removed. A `Break`
cycle break warns with the full cycle and the dropped edge, and names the
reason when edge derivation recorded one.

### Diagnostics

- **Omit a source position when no position is accurate.** Synthesized or
  many-to-one-folded nodes remain positionless instead of referring to a
  line whose text the diagnostic doesn't describe. Absence-shaped
  diagnostics ("missing required", op/conflict messages spanning two source
  texts) carry no line by nature. A dangling-ref diagnostic points at the
  referring line, which is the line the author must fix.
- **Line numbers index the original text**, so `transform.TextRule` maps one
  line to one line and no rule can change the line count: `DropLines` blanks
  instead of removing. Raw and transformed block spans therefore always align.
- **Collect all diagnostics instead of failing at the first error.** `Drop`
  warns and drops unknown input, merge resolvers settle contested slots, and
  `Break` warns and drops an ordering edge. Invalid values, unresolved
  references, and duplicate keys are Errors.

### Modeling decisions

- **`shutdown` / `no shutdown` stay two literal toggle nodes.** A tri-state
  node would need the platform's default administrative state, which depends
  on the platform and port. The engine compares explicit text. Toggle
  deduplication runs before the Protected check because a flip is a value
  change.
- **`.List` implies `Idempotent`** (re-applying a set is a no-op, and it
  places the node in the Modify pairing category the delta machinery uses).
- **`EmptyOnRemove` is per definition, not per Kind** because its mode-like,
  always-present section declares no Kind.
- **Tags are non-identity labels.** Relations match a node's Kind and Tags.
  Identity and pairing use only Kind, but ordering also uses Tags. Sibling
  relations compare direct children of one parent; top-level nodes share the
  sentinel root. Use `Toggles` for alternate spellings of one setting and
  `ExcludeTag` for many-to-many mode splits such as L2 versus L3.
- **A Tag must not shadow a Kind.** A name cannot be both identity-bearing and
  non-identity metadata. `ValidateRelations` also rejects undeclared labels,
  incomplete key matches, unsupported relation shapes, and missing target
  keys.
- Blocks, lists, `Members`, `RespellAs`, and `Key` are mutually exclusive in
  documented combinations that cannot represent valid grammar. Invalid
  combinations panic in both call orders.
