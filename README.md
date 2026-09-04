# confetti

confetti is a schema-aware, offline engine for network-device CLI
configurations: parse, validate (Juniper-style commit check), canonicalize,
remediate, roll back, diff, and merge. It processes text without device
connections. The core contains no vendor-specific logic.

- **Offline and deterministic.** Everything runs on text you already have;
  the same inputs always produce the same artifact.
- **Grammar as data.** A platform is a `schema.Schema` you author in Go:
  line templates with typed captures, sections, cross-references, negation
  and reset forms, raw blocks (banners), toggle pairs, list-valued args,
  dual-form spellings, protected nodes.
- **Safety constraints.** Remediation artifacts are dependency-ordered
  (definitions before referrers on add, referrers first on remove),
  and protected nodes cannot be deleted. Rejected deletions and aborted
  ordering cycles produce no artifact.

## Install

```bash
go get github.com/acidsailor/confetti
```

## Usage

Build an `Engine` from a `schema.Schema` describing your platform's
grammar, then drive it with text:

```go
e := confetti.New(mySchema)

// Parse and check referential integrity.
cfg, diags := e.Import(runningText)
diags = e.CommitCheck(cfg)

// Render the canonical form.
out, diags := e.Render(cfg)

// Generate CLI commands that change running into intended.
res, diags := e.Remediate(running, intended)
artifact, _ := e.Render(res.Tree)

// Generate the inverse artifact with the same argument order.
inv, diags := e.Rollback(running, intended)

// Generate a git-diff-style view without a commit check.
view, diags := e.Compare(running, intended)

// Merge fragments with schema-declared conflict handling.
merged, diags := e.Merge(merge.Options{}, base, overlay)
diags = e.CommitCheck(merged)
```

A schema is declared as data:

```go
s := schema.New()
vlan := s.Node("vlan {{ id:vlan }}").Card(schema.ZeroToN).Kind("vlan").Key("id")
vlan.Child("name {{ text:word }}").Card(schema.ZeroToOne).MarkIdempotent()
iface := s.Node("interface {{ name:ifname }}").Card(schema.ZeroToN)
iface.Child("switchport access vlan {{ vlan:vlan }}").
	Card(schema.ZeroToOne).MarkIdempotent().Ref("vlan", "vlan.id")
```

`confetti.WithUnknown(parse.Drop)` warns and drops unmodeled commands during
import. `confetti.WithCycle(remediate.Break)` warns and drops an ordering edge
to break a cycle.

Use `WithBaseline` for objects the device provides but never prints, such as
the default VRF or built-in class maps. The text imports with the same schema
and transforms. Baseline objects resolve references and prerequisites, and
they never render, merge, or appear in a remediation. A plan that would negate
one reports an error. `New` panics if the baseline text does not import
cleanly, because it is authored platform data:

```go
e := confetti.New(mySchema, confetti.WithBaseline("vlan 1\nvrf context default\n"))
```

Use `WithCommitChecks` for whole-tree rules that do not fit the schema. The
validators run after the built-in check in `CommitCheck`, `Remediate`, and
`Rollback`. A validator receives the tree and the baseline, reports
diagnostics, and must not modify either tree:

```go
e := confetti.New(mySchema, confetti.WithCommitChecks(checkPlatformRules))
```

See [example_test.go](example_test.go) for a runnable schema. The schemas in
`internal/fixture/` cover additional features but model imaginary platforms and
are not suitable for real devices. Define production platform schemas in
downstream repositories.

## Documentation

- [docs/design.md](docs/design.md) describes the architecture, pipelines,
  and design invariants.
- [docs/fixtures.md](docs/fixtures.md) records fixture grammar lineage and
  unverified assumptions.
- Package-level `doc.go` files cover each package's role and contracts.

## License

Apache-2.0. See [LICENSE](LICENSE).
