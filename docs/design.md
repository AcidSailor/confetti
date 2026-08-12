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
├── schema        grammar-as-data: node templates, kinds/keys/refs, negate/
│                 block/list/toggle/fold strategies, MatchChild
├── value         value-type registry (builtins: word, rest, uint)
├── parse         text → tree (indent stack, block capture, BlockSpans) and
│                 the import fold (Respell → ListContinues → Members)
├── tree          the config tree; Node stores op tags, lines, and block bodies
├── validate      ImportCheck (values, cardinality, dup keys, toggles,
│                 required children) and CommitCheck (refs, Requires)
├── render        tree → canonical text
├── transform     text rules (DropLines, PerLineSub) + tree transform seam
├── remediate     Diff: pair → collect ops → derive edges → schedule →
│                 materialize; Result{Tree, Changes}
├── graph         ordering graph passed to schema.OrderHook; pure leaf
│                 package, stdlib only (see Layering)
├── compare       Changes → git-diff-style human view
├── merge         fragments → one tree, conflict policy + list union
├── diag          two-severity diagnostics with optional source line
└── internal/
    ├── ident     node-pairing identity shared by remediate + merge
    ├── listval   the one comma+range list codec
    ├── testtypes domain value types for core tests
    ├── valcheck  the shared ipv4 and numeric-range value checks
    └── fixture/{alpha,beta}  imaginary-platform schemas (see fixtures.md)
```

Layering rules:

- **`graph` is a leaf package** with no confetti imports. The import graph
  enforces this rule: `tree` imports `schema`, and `schema` uses `graph` types
  in the `OrderHook` signature. Importing either package from `graph` would
  cause a cycle. `remediate` keeps detailed per-operation state in a parallel
  slice indexed by operation index.
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
CommitCheck: refs and Requires over an assembled tree
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

- **`diag.Policy.Strict` controls tolerance for unknown input. It does not
  relax constraints on schema-known configuration.** Lenient mode lets existing
  configurations with unmodeled lines import. After a line matches, all
  schema constraints apply. Fold and authoring errors during Diff are Errors
  in both policies.
- **`Protect()` fails in both policies.** A policy setting cannot disable
  this safety constraint. Silent skips hide attempted deletion. The check
  applies to every deletion path: the removal loop, `ckReplace`, removed
  subtree descendants, and keyed identity changes. `ckReplace` refuses both
  operations because an add without its removal could create duplicates.
- **The Protected boundary: value changes are not deletions.** `ckModify`,
  a toggle flip, and list deltas on a protected node stay allowed.
- **Protected is all-or-nothing for header negation but per-child for an
  `EmptyOnRemove` expansion**, and `Protected` × `EmptyOnRemove` on one def
  is an authoring panic ("never delete this" contradicts "here is how to
  delete this").
- **An Error prevents artifact output.** A strict ordering cycle produces an
  empty tree and empty Changes. A Protected refusal produces no operation or
  Change. A partial artifact would be unsafe.

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
- **Whole-tree validators belong to `Engine`.** They consume `*tree.Config`,
  and `tree` imports `schema`; storing them in `Schema` would create an import
  cycle. `WithCommitChecks` appends validators after the built-in
  `CommitCheck`, in registration order. They run for `CommitCheck`,
  `Remediate` (intended), and `Rollback` (running). `Render`, `Compare`, and
  `Merge` skip them. A validator reports into its own `diag.Diagnostics`,
  merged after it returns, so it cannot drop what earlier checks recorded. It
  must not modify the tree: `Remediate` validates the caller's `intended`
  before `Diff`, so a mutation would silently change the remediation. A nil
  validator panics at registration.
- **Toggle pairs are declared, never text-detected.** The `"no "`-prefix
  heuristic has no fallback. A test verifies that an undeclared pair emits
  separate remove and add operations.

### Pairing and diff semantics

- **Remediation output is a `*tree.Config` of new operation-tagged nodes.**
  The existing renderer produces executable CLI, and `tree.Walk` with `Op()`
  supports inspection. `OpNone` must remain the zero value because every
  tree node has an operation field. `OpRemove` nodes
  carry `def == nil`: a negated line does not match its positive definition,
  and the missing definition suppresses an incorrect section-exit token on a
  negated block.
- **Diff never mutates its inputs.** `Change.Running`/`.Intended` alias the
  caller's trees read-only; the artifact is built from new nodes.
- **`Diff` is direction-independent, and `Remediate` and `Rollback` use the
  same argument order: `running, intended`.** Change the method name, not the
  argument order, to request rollback. This remains valid only while pipeline
  logic does not assign device-specific meaning to an argument. A `Result`
  cannot be inverted because OpRemove has no definition and OpModify stores
  only the new value.
- **Pairing identity uses Kind instead of definition pointers.** A keyed node
  that changes between sibling templates must pair as one entity. Otherwise,
  a rename would add and then remove the entity. **A non-keyed,
  single-occupancy, non-toggle node with a Kind pairs by Kind alone**
  (`KindedSingle`): variant spellings of one slot become one Replace
  (negate, then add) instead of an Add plus an unrelated Remove that empties
  the slot on the device. Toggle members are excluded because a flip already
  supersedes its partner's removal, and `EmptyOnRemove` sections are
  excluded because they have no header negation to pair with, so adding a
  Kind for Ref resolution never changes how such a section remediates.
  A running config holding multiple spellings of one Kind slot negates stale
  spellings before it reissues the intended value; two intended spellings are
  an ambiguous goal reported at policy severity, and only the first applies.
  **Block bodies are not part of
  identity.** A body-only change must be Modify, not separate Add and
  Remove operations. **Merge extends pairing in one way:** each non-keyed
  ZeroToOne definition without a Kind, or in a toggle group, is one slot
  per level. Remediation
  must not pair by definition alone because an unmarked pair can be
  genuinely independent siblings; pairing needs the explicit Kind signal.
  Diff reports at policy severity when an Add and a Remove still land on
  one single-occupancy slot (same definition, or the same Kind shared
  across a toggle member and a non-toggle sibling), and `ImportCheck`
  rejects two spellings of one Kind-paired slot at the same level.
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
  conflicts, and `Requires`) and is topologically sorted.** Use `OrderHook`
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
- **List semantics have three views.** `Parts` returns explicit input items
  for validation and reference checks. `Resolve` returns the semantic set for
  diff, union, and ordering. `Canonical` returns the shortest output spelling.
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
  dropped by lenient import cannot be restored.
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
check references in running configuration that will be removed. A lenient
cycle break warns about its partial-application risk and names the dependency.

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
- **Collect all diagnostics instead of failing at the first error.** `Strict`
  selects a per-stage recovery strategy for unknown or conflicting input:
  `parse` drops an unmatched line when lenient, `merge` lets the later part
  win, and `remediate` breaks an ordering cycle instead of aborting. Invalid
  values, unresolved references, and duplicate keys are always Errors.

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
- Blocks, lists, `Members`, `RespellAs`, and `Key` are mutually exclusive in
  documented combinations that cannot represent valid grammar. Invalid
  combinations panic in both call orders.
