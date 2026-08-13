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
  protected nodes refuse deletion under every option value, and on any Error no
  artifact is returned.

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

// Merge fragments. The default resolver follows each slot's declared merge
// kind; merge.Refuse rejects undeclared conflicts instead.
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

`confetti.WithUnknown(parse.Drop)` downgrades unknown commands to warnings so
existing configurations with unmodeled lines import;
`confetti.WithCycle(remediate.Break)` lets remediation break an ordering
cycle instead of aborting.

Use `WithCommitChecks` for whole-tree rules that do not fit the schema. The
validators run after the built-in check in `CommitCheck`, `Remediate`, and
`Rollback`. A validator reports diagnostics and must not modify the tree:

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
