package confetti

import (
	"strings"

	"github.com/acidsailor/confetti/compare"
	"github.com/acidsailor/confetti/diag"
	"github.com/acidsailor/confetti/merge"
	"github.com/acidsailor/confetti/parse"
	"github.com/acidsailor/confetti/remediate"
	"github.com/acidsailor/confetti/render"
	"github.com/acidsailor/confetti/schema"
	"github.com/acidsailor/confetti/transform"
	"github.com/acidsailor/confetti/validate"
)

// Engine runs configuration pipelines for one schema.
type Engine struct {
	schema       *schema.Schema
	unknown      parse.Unknown
	cycle        remediate.Cycle
	importText   []transform.TextRule
	exportText   []transform.TextRule
	importTree   []transform.TreeTransform
	exportTree   []transform.TreeTransform
	commitChecks []Validator
	// baselineText is used only by New.
	baselineText string
	// baseline holds device-provided objects for relation checks and removal guards.
	baseline *schema.Config
}

// Option configures an Engine.
type Option func(*Engine)

// WithUnknown sets Import's unknown-command behavior; the default is parse.Reject.
func WithUnknown(u parse.Unknown) Option {
	return func(e *Engine) { e.unknown = u }
}

// WithCycle sets cycle handling for Remediate, Rollback, and Compare; the default is remediate.Abort.
func WithCycle(c remediate.Cycle) Option {
	return func(e *Engine) { e.cycle = c }
}

// WithImportText appends text transforms that run before parsing.
func WithImportText(rules ...transform.TextRule) Option {
	return func(e *Engine) { e.importText = append(e.importText, rules...) }
}

// WithExportText appends text transforms that run after rendering.
func WithExportText(rules ...transform.TextRule) Option {
	return func(e *Engine) { e.exportText = append(e.exportText, rules...) }
}

// WithImportTree appends tree transforms that run after import folds.
func WithImportTree(ts ...transform.TreeTransform) Option {
	return func(e *Engine) { e.importTree = append(e.importTree, ts...) }
}

// WithExportTree appends tree transforms that run before rendering.
func WithExportTree(ts ...transform.TreeTransform) Option {
	return func(e *Engine) { e.exportTree = append(e.exportTree, ts...) }
}

// Validator checks cfg against a non-nil baseline copy and reports into d without modifying cfg.
type Validator func(cfg, baseline *schema.Config, d *diag.Diagnostics)

// WithCommitChecks appends whole-tree validators that run after the built-in commit check.
func WithCommitChecks(fns ...Validator) Option {
	for _, fn := range fns {
		if fn == nil {
			panic("confetti: WithCommitChecks with nil func")
		}
	}
	return func(e *Engine) { e.commitChecks = append(e.commitChecks, fns...) }
}

// WithBaseline appends text for device-provided objects absent from printed configuration.
func WithBaseline(text string) Option {
	// Terminate every fragment so appending cannot merge two lines.
	return func(e *Engine) {
		e.baselineText += strings.TrimRight(text, "\n") + "\n"
	}
}

// New constructs an Engine for the given schema and panics when the baseline reports any diagnostic.
func New(s *schema.Schema, opts ...Option) *Engine {
	e := &Engine{schema: s}
	for _, o := range opts {
		o(e)
	}
	// Consumers always receive a non-nil baseline.
	e.baseline = schema.NewConfig(s)
	// Parse after every option so import transforms apply regardless of option order.
	if e.baselineText != "" {
		d := diag.New()
		e.baseline = e.importWith(
			e.baselineText,
			parse.Reject,
			validate.FragmentCheck,
			d,
		)
		// Baseline warnings also indicate schema authoring errors.
		if len(d.Items) > 0 {
			panic("confetti: baseline does not import cleanly:\n" + d.String())
		}
	}
	return e
}

// Import transforms, parses, folds, and validates configuration text.
func (e *Engine) Import(text string) (*schema.Config, *diag.Diagnostics) {
	d := diag.New()
	return e.importWith(text, e.unknown, validate.ImportCheck, d), d
}

// importWith runs the import pipeline with an explicit unknown-command policy and final check.
func (e *Engine) importWith(
	text string,
	unknown parse.Unknown,
	check func(*schema.Config, *diag.Diagnostics),
	d *diag.Diagnostics,
) *schema.Config {
	text = applyTextOutsideBlocks(e.schema, e.importText, text)
	cfg := parse.Parse(e.schema, text, unknown, d)
	parse.Fold(cfg, d)
	transform.ApplyTree(e.importTree, cfg)
	check(cfg, d)
	return cfg
}

// applyTextOutsideBlocks preserves block spans detected before or after text transforms.
func applyTextOutsideBlocks(
	s *schema.Schema,
	rules []transform.TextRule,
	text string,
) string {
	if len(rules) == 0 {
		return text
	}
	cooked := transform.ApplyText(rules, text)
	spans := parse.BlockSpans(s, text)
	if spans == nil {
		return cooked
	}
	// Text rules preserve line counts, so both span maps use the same indexes.
	for i, b := range parse.BlockSpans(s, cooked) {
		spans[i] = spans[i] || b
	}
	lines, cookedLines := strings.Split(text, "\n"), strings.Split(cooked, "\n")
	for i := range lines {
		if !spans[i] {
			lines[i] = cookedLines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// CommitCheck runs built-in and registered checks against an assembled tree.
func (e *Engine) CommitCheck(cfg *schema.Config) *diag.Diagnostics {
	d := diag.New()
	e.commitCheck(cfg, d)
	return d
}

func (e *Engine) commitCheck(cfg *schema.Config, d *diag.Diagnostics) {
	validate.CommitCheck(cfg, e.baseline, d)
	if len(e.commitChecks) == 0 {
		return
	}
	base := e.baseline
	// A tree from another schema cannot use this baseline; CommitCheck already reported the mismatch.
	if base.Schema != cfg.Schema {
		base = schema.NewConfig(cfg.Schema)
	}
	// Separate collectors prevent validators from clearing earlier diagnostics.
	for _, fn := range e.commitChecks {
		vd := diag.New()
		// A clone protects the shared baseline from validator mutations.
		fn(cfg, schema.CloneConfig(base), vd)
		d.Merge(vd)
	}
}

// Render applies tree transforms, renders canonical text, and applies text rules outside raw blocks.
func (e *Engine) Render(cfg *schema.Config) (string, *diag.Diagnostics) {
	d := diag.New()
	transform.ApplyTree(e.exportTree, cfg)
	out := render.Render(cfg)
	out = applyTextOutsideBlocks(e.schema, e.exportText, out)
	return out, d
}

// Remediate checks intended and returns the operation-tagged difference from running to intended.
func (e *Engine) Remediate(
	running, intended *schema.Config,
) (*remediate.Result, *diag.Diagnostics) {
	d := diag.New()
	e.commitCheck(intended, d)
	res, rd := remediate.Diff(running, intended, e.diffOptions())
	d.Merge(rd)
	return res, d
}

// Rollback checks running and plans its restoration from intended, using canonical parsed content.
func (e *Engine) Rollback(
	running, intended *schema.Config,
) (*remediate.Result, *diag.Diagnostics) {
	d := diag.New()
	e.commitCheck(running, d)
	res, rd := remediate.Diff(intended, running, e.diffOptions())
	d.Merge(rd)
	return res, d
}

// Compare returns a git-diff-style view without commit-checking either input.
func (e *Engine) Compare(
	running, intended *schema.Config,
) (string, *diag.Diagnostics) {
	res, d := remediate.Diff(running, intended, e.diffOptions())
	return compare.Render(res.Changes), d
}

// diffOptions returns the Diff options this engine was built with.
func (e *Engine) diffOptions() remediate.Options {
	return remediate.Options{Cycle: e.cycle, Baseline: e.baseline}
}

// Merge combines fragments in order with per-call conflict resolution and no commit check.
func (e *Engine) Merge(
	opts merge.Options,
	parts ...*schema.Config,
) (*schema.Config, *diag.Diagnostics) {
	return merge.Merge(e.schema, opts, parts...)
}
