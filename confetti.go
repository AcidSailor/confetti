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
	"github.com/acidsailor/confetti/tree"
	"github.com/acidsailor/confetti/validate"
)

// Engine ties a schema, a policy, and the import/export transform pipelines.
type Engine struct {
	schema       *schema.Schema
	policy       diag.Policy
	importText   []transform.TextRule
	exportText   []transform.TextRule
	importTree   []transform.TreeTransform
	exportTree   []transform.TreeTransform
	commitChecks []func(*tree.Config, *diag.Diagnostics)
}

// Option configures an Engine.
type Option func(*Engine)

// WithPolicy sets the strict/lenient policy.
func WithPolicy(p diag.Policy) Option {
	return func(e *Engine) { e.policy = p }
}

// WithImportText appends import-side (pre-parse) text transforms.
func WithImportText(rules ...transform.TextRule) Option {
	return func(e *Engine) { e.importText = append(e.importText, rules...) }
}

// WithExportText appends export-side (post-render) text transforms.
func WithExportText(rules ...transform.TextRule) Option {
	return func(e *Engine) { e.exportText = append(e.exportText, rules...) }
}

// WithImportTree appends import-side (post-parse) tree transforms.
func WithImportTree(ts ...transform.TreeTransform) Option {
	return func(e *Engine) { e.importTree = append(e.importTree, ts...) }
}

// WithExportTree appends export-side (pre-render) tree transforms.
func WithExportTree(ts ...transform.TreeTransform) Option {
	return func(e *Engine) { e.exportTree = append(e.exportTree, ts...) }
}

// WithCommitChecks appends whole-tree validators, which report into d and must not modify cfg, after the built-in commit check.
func WithCommitChecks(fns ...func(*tree.Config, *diag.Diagnostics)) Option {
	for _, fn := range fns {
		if fn == nil {
			panic("confetti: WithCommitChecks with nil func")
		}
	}
	return func(e *Engine) { e.commitChecks = append(e.commitChecks, fns...) }
}

// New constructs an Engine for the given schema.
func New(s *schema.Schema, opts ...Option) *Engine {
	e := &Engine{schema: s}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Import transforms, parses, folds, and validates configuration text.
func (e *Engine) Import(text string) (*tree.Config, *diag.Diagnostics) {
	d := diag.New()
	text = applyTextOutsideBlocks(e.schema, e.importText, text)
	cfg := parse.Parse(e.schema, text, e.policy, d)
	parse.Fold(cfg, d)
	transform.ApplyTree(e.importTree, cfg)
	validate.ImportCheck(cfg, d)
	return cfg, d
}

// applyTextOutsideBlocks protects spans found before or after text rules so a rule cannot alter a block body or remove its terminator.
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
	// Rules preserve line counts, so raw and cooked spans align index-for-index.
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
func (e *Engine) CommitCheck(cfg *tree.Config) *diag.Diagnostics {
	d := diag.New()
	e.commitCheck(cfg, d)
	return d
}

func (e *Engine) commitCheck(cfg *tree.Config, d *diag.Diagnostics) {
	validate.CommitCheck(cfg, d)
	// Each validator collects into its own Diagnostics so it cannot drop what earlier checks recorded.
	for _, fn := range e.commitChecks {
		vd := diag.New()
		fn(cfg, vd)
		d.Merge(vd)
	}
}

// Render applies tree transforms, renders canonical text, and applies text rules outside raw blocks.
func (e *Engine) Render(cfg *tree.Config) (string, *diag.Diagnostics) {
	d := diag.New()
	transform.ApplyTree(e.exportTree, cfg)
	out := render.Render(cfg)
	out = applyTextOutsideBlocks(e.schema, e.exportText, out)
	return out, d
}

// Remediate checks intended and returns the operation-tagged difference from running to intended.
func (e *Engine) Remediate(
	running, intended *tree.Config,
) (*remediate.Result, *diag.Diagnostics) {
	d := diag.New()
	e.commitCheck(intended, d)
	res, rd := remediate.Diff(running, intended, e.policy)
	d.Merge(rd)
	return res, d
}

// Rollback checks running and returns the inverse of Remediate with the same argument order; restoration uses canonical parsed content, not original bytes.
func (e *Engine) Rollback(
	running, intended *tree.Config,
) (*remediate.Result, *diag.Diagnostics) {
	d := diag.New()
	e.commitCheck(running, d)
	res, rd := remediate.Diff(intended, running, e.policy)
	d.Merge(rd)
	return res, d
}

// Compare returns a git-diff-style view without commit-checking either input.
func (e *Engine) Compare(
	running, intended *tree.Config,
) (string, *diag.Diagnostics) {
	res, d := remediate.Diff(running, intended, e.policy)
	return compare.Render(res.Changes), d
}

// Merge combines fragments in order without running a commit check; strict conflicts keep the first value, lenient conflicts keep the last, and list slots form a union.
func (e *Engine) Merge(
	parts ...*tree.Config,
) (*tree.Config, *diag.Diagnostics) {
	return merge.Merge(e.schema, e.policy, parts...)
}
