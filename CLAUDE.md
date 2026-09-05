# CLAUDE.md

confetti is a schema-aware, offline engine for network-device CLI
configurations. It is a pure library with no runtime dependencies. Tests use
testify. The license is Apache-2.0.

## Read first

- `docs/design.md` describes the architecture, pipelines, and invariants.
  Read the invariants before you change remediation, merge, parse folds, or
  schema builders.
- `docs/fixtures.md` explains where the fixture grammars came from and which
  parts are unverified against real devices.

## Commands

```bash
task test     # go test -race ./...
task ci       # Check formatting and lint without changing files.
task lint     # Apply formatting and lint fixes; keep the fixes.
```

Keep automatic fixes from `task lint`; `task ci` checks them. Run the full
test suite with `-race` before you merge.

Run parser fuzzing with `go test -run '^$' -fuzz FuzzParse ./parse/`.

## Review constraints

- **MatchChild validation**: each path that assigns a definition and text to
  a tree node must match the text against all candidates at that level with
  `schema.MatchChild`. It must bind the intended definition. Matching only
  that definition can produce a different identity after parsing and cancel
  its own remediation.
- **Both call orders**: schema-builder exclusions and prerequisites must hold
  for either method-call order. Test both orders.
- **Every caller of a primitive**: an invariant must hold on every path to
  the primitive. Search for all callers before the change is complete.
- **Explicit degradation**: report a `diag.Error`, Warning, or authoring
  panic instead of silently keeping or skipping input. Policy can tolerate
  unknown input, but it does not relax constraints on schema-known input.

## Development process

- Feature branches (`feat/…`, `fix/…`), conventional commits (svu drives
  releases: `fix:`→patch, `feat:`→minor, `!`→major; `chore:`/`docs:`→none).
- Inspect the code before you propose a design. Agree on one proposal, then
  implement it in test-driven slices. Review the complete branch before you
  merge. Verify review findings with probes and add regression tests for
  confirmed defects.
- Reviewer agents can leave probe files (`zzprobe*`). Check `git status`
  after any review session.
- Fast-forward merge to `main`, delete the branch.
- Goldens are byte-exact contracts. A golden diff is a behavior change.

## Layout notes

- Fixtures (`internal/fixture/{alpha,beta}`) describe imaginary platforms and
  are the only in-repository schemas. They cannot be imported outside the
  module. Put real platform packages in downstream repositories.
- `graph` must remain a leaf package with standard-library imports only. The
  import graph requires this constraint. See `docs/design.md`.
