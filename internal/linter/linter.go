// Package linter schedules the passes over a file and applies fixes.
package linter

import (
	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/fix"
	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/rules"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/suppress"
	"github.com/ppreeper/rusq/internal/templater"
)

// Options controls a run.
type Options struct {
	Fix         bool
	UnsafeFixes bool
	FixOnly     bool
	// Index enables cross-file rules (SPEC §8.3). Nil disables them.
	Index *index.Index
}

// Result is the outcome for one file.
type Result struct {
	Path         string
	Diagnostics  []diagnostic.Diagnostic
	Fixed        []string
	FixedSource  []byte
	Changed      bool
	SyntaxErrors []diagnostic.SyntaxError
}

// Linter runs the selected rules.
type Linter struct {
	Settings settings.Settings
	rules    []rules.Rule
	suppress bool
}

// New builds a linter for the settings.
func New(s settings.Settings) *Linter {
	l := &Linter{Settings: s, suppress: !s.IgnoreSuppressions && !s.DisableNoqa}
	for _, r := range rules.All() {
		m := r.Meta()
		if len(m.Dialects) > 0 && !containsDialect(m.Dialects, s.DialectName) {
			continue
		}
		if !s.IsSelected(m.Code, m.Name, m.Family, string(m.Category), m.Default) {
			continue
		}
		if !m.Stable && !s.Preview {
			continue
		}
		l.rules = append(l.rules, r)
	}
	return l
}

func containsDialect(list []string, name string) bool {
	for _, d := range list {
		if d == name {
			return true
		}
	}
	return false
}

// Run lints source and, when requested, applies fixes.
func (l *Linter) Run(src source.Text, opts Options) Result {
	if !opts.Fix {
		return l.lintOnce(src, opts.Index)
	}
	return l.fixAll(src, opts)
}

func (l *Linter) fixAll(src source.Text, opts Options) Result {
	cur := src
	var fixed []string
	var last Result
	for iter := 0; iter < 100; iter++ {
		res := l.lintOnce(cur, opts.Index)
		last = res
		if len(res.SyntaxErrors) > 0 && iter == 0 {
			// Never apply edits to a file we could not fully lex/parse.
			res.Fixed = fixed
			res.FixedSource = cur.Bytes
			res.Changed = len(fixed) > 0
			return res
		}
		newBytes, applied := fix.Apply(cur.Bytes, res.Diagnostics, opts.UnsafeFixes)
		if len(applied) == 0 {
			res.Fixed = fixed
			res.FixedSource = cur.Bytes
			res.Changed = len(fixed) > 0
			if opts.FixOnly {
				res.Diagnostics = nil
			}
			return res
		}
		// Re-parse to ensure the pass introduced no new parse error.
		check := l.parseOnly(source.New(cur.Path, newBytes))
		if len(check) > len(res.SyntaxErrors) {
			break
		}
		for _, a := range applied {
			fixed = append(fixed, a.Code)
		}
		cur = source.New(cur.Path, newBytes)
	}
	last.Fixed = fixed
	last.FixedSource = cur.Bytes
	last.Changed = len(fixed) > 0
	if opts.FixOnly {
		last.Diagnostics = nil
	}
	return last
}

func (l *Linter) parseOnly(src source.Text) []diagnostic.SyntaxError {
	t := templater.New(l.Settings.Templater)
	variants, errs := t.Render(src, l.Settings)
	out := append([]diagnostic.SyntaxError{}, errs...)
	if len(variants) == 0 {
		return out
	}
	v := variants[0]
	rendered := source.New(src.Path, v.Rendered)
	tokens, lexErrs := lexer.Lex(rendered, l.Settings.Dialect)
	_, perrs := parser.New().Parse(tokens, l.Settings.Dialect)
	for _, e := range append(lexErrs, perrs...) {
		out = append(out, v.MapError(e))
	}
	return out
}

// rawOnly lists the rules that read the raw, unrendered source: JJ01 inspects
// template tag padding itself, and RF08 looks for ref()/source() calls that the
// renderer would otherwise resolve away.
var rawOnly = map[string]bool{"JJ01": true, "RF08": true}

func (l *Linter) lintOnce(src source.Text, ix *index.Index) Result {
	res := Result{Path: src.Path}
	rawTokens, _ := lexer.Lex(src, l.Settings.Dialect)
	loc := source.NewLocator(src.Bytes)

	t := templater.New(l.Settings.Templater)
	variants, terr := t.Render(src, l.Settings)
	res.SyntaxErrors = append(res.SyntaxErrors, terr...)
	if len(variants) == 0 {
		variants = []templater.Variant{{
			Rendered:   src.Bytes,
			Provenance: []string{"raw"},
		}}
	}

	seen := map[string]int{}
	primary := true
	for _, v := range variants {
		ds, errs := l.lintVariant(src, v, ix)
		if primary {
			res.SyntaxErrors = append(res.SyntaxErrors, errs...)
			primary = false
		}
		for _, d := range ds {
			k := d.Code + "|" + itoa(d.Span.Start) + "|" + itoa(d.Span.End) + "|" + d.Message
			if idx, ok := seen[k]; ok {
				res.Diagnostics[idx].Provenance = mergeProvenance(res.Diagnostics[idx].Provenance, d.Provenance)
				continue
			}
			seen[k] = len(res.Diagnostics)
			res.Diagnostics = append(res.Diagnostics, d)
		}
	}

	// Raw-source rules run once, independent of rendering.
	res.Diagnostics = append(res.Diagnostics, l.lintRawOnly(src, rawTokens, loc, ix)...)

	if l.suppress {
		dirs := suppress.Parse(rawTokens)
		res.Diagnostics, dirs = l.applySuppressions(res.Diagnostics, dirs, rawTokens, loc)
		l.emitUnusedSuppressions(&res, dirs)
	}

	diagnostic.Sort(res.Diagnostics)
	return res
}

func (l *Linter) lintVariant(src source.Text, v templater.Variant, ix *index.Index) ([]diagnostic.Diagnostic, []diagnostic.SyntaxError) {
	rendered := source.New(src.Path, v.Rendered)
	tokens, lexErrs := lexer.Lex(rendered, l.Settings.Dialect)
	tr, parseErrs := parser.New().Parse(tokens, l.Settings.Dialect)
	errs := append(lexErrs, parseErrs...)
	if tr != nil {
		tr.Errors = errs
	}
	loc := source.NewLocator(rendered.Bytes)
	lines := lint.NewLines(tokens, loc)
	file := ast.Project(tr)

	ctx := &lint.Context{
		Source:   rendered,
		Locator:  loc,
		Dialect:  l.Settings.Dialect,
		Settings: l.Settings,
		Tokens:   tokens,
		Tree:     tr,
		AST:      file,
		Lines:    lines,
		Index:    ix,
	}
	var out []diagnostic.Diagnostic
	for _, r := range l.rules {
		m := r.Meta()
		if rawOnly[m.Code] {
			continue // raw-source only
		}
		for _, d := range r.Check(ctx) {
			if d.Fixability == diagnostic.NoFix && d.Fix != nil {
				d.Fixability = m.Fixable
			}
			d = v.MapDiagnostic(d)
			d.Provenance = append(d.Provenance, v.Provenance...)
			if l.Settings.IsWarning(d.Code, d.Name, m.Family, string(m.Category)) {
				d.Severity = diagnostic.Warning
			}
			out = append(out, d)
		}
	}
	return out, errs
}

func (l *Linter) lintRawOnly(src source.Text, tokens []lexer.Token, loc *source.Locator, ix *index.Index) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	ctx := &lint.Context{
		Source:   src,
		Locator:  loc,
		Dialect:  l.Settings.Dialect,
		Settings: l.Settings,
		Tokens:   tokens,
		Index:    ix,
	}
	for _, r := range l.rules {
		m := r.Meta()
		if !rawOnly[m.Code] {
			continue
		}
		for _, d := range r.Check(ctx) {
			if l.Settings.IsWarning(d.Code, d.Name, m.Family, string(m.Category)) {
				d.Severity = diagnostic.Warning
			}
			out = append(out, d)
		}
	}
	return out
}

func mergeProvenance(a, b []string) []string {
	for _, s := range b {
		found := false
		for _, x := range a {
			if x == s {
				found = true
				break
			}
		}
		if !found {
			a = append(a, s)
		}
	}
	return a
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
