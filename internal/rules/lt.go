package rules

import (
	"bytes"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

// LT05 — line length.
type lt05 struct{ meta Meta }

func (r *lt05) Meta() Meta { return r.meta }
func (r *lt05) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	limit := ctx.Settings.LineLength
	ignoreComments := ctx.Settings.RuleBool(r.meta.Name, "ignore-comment-lines", false)
	var out []diagnostic.Diagnostic
	for _, line := range ctx.Lines.Physical {
		if limit <= 0 || runewidth.StringWidth(line.Text) <= limit {
			continue
		}
		if ignoreComments && strings.HasPrefix(strings.TrimSpace(line.Text), "--") {
			continue
		}
		out = append(out, diag(r.meta, source.Range{Start: line.Start, End: line.End},
			"Line is too long ("+itoa(runewidth.StringWidth(line.Text))+" > "+itoa(limit)+")."))
	}
	return out
}

// LT12 — single trailing newline.
type lt12 struct{ meta Meta }

func (r *lt12) Meta() Meta { return r.meta }
func (r *lt12) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	b := ctx.Source.Bytes
	if len(b) == 0 {
		return nil
	}
	if !bytes.HasSuffix(b, []byte("\n")) {
		d := diag(r.meta, source.NewRangeAt(len(b)), "File must end with a single trailing newline.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(len(b)), "\n")}}
		d.Fixability = r.meta.Fixable
		return []diagnostic.Diagnostic{d}
	}
	n := 0
	for n < len(b) && b[len(b)-1-n] == '\n' {
		n++
	}
	if n > 1 {
		span := source.Range{Start: len(b) - n + 1, End: len(b)}
		d := diag(r.meta, span, "File must end with a single trailing newline.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, "")}}
		d.Fixability = r.meta.Fixable
		return []diagnostic.Diagnostic{d}
	}
	return nil
}

// LT13 — no leading whitespace.
type lt13 struct{ meta Meta }

func (r *lt13) Meta() Meta { return r.meta }
func (r *lt13) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	b := ctx.Source.Bytes
	i := 0
	for i < len(b) {
		switch b[i] {
		case ' ', '\t', '\r', '\n', '\f', '\v':
			i++
			continue
		}
		break
	}
	if i == 0 || i >= len(b) {
		return nil
	}
	span := source.Range{Start: 0, End: i}
	d := diag(r.meta, span, "File must not begin with newlines or whitespace.")
	d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, "")}}
	d.Fixability = r.meta.Fixable
	return []diagnostic.Diagnostic{d}
}

// LT15 — too many consecutive blank lines.
type lt15 struct{ meta Meta }

func (r *lt15) Meta() Meta { return r.meta }
func (r *lt15) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	max := ctx.Settings.RuleInt(r.meta.Name, "maximum-empty-lines", ctx.Settings.Format.MaxBlankLines)
	if max < 0 {
		return nil
	}
	allowed := max + 1 // N blank lines are N+1 newline tokens
	var out []diagnostic.Diagnostic
	run := 0
	start := -1
	flush := func(end int) {
		if run > allowed && start >= 0 {
			span := source.Range{Start: ctx.Tokens[start].Span.Start, End: ctx.Tokens[end].Span.End}
			repl := strings.Repeat(ctx.Tokens[start].Text, allowed)
			d := diag(r.meta, span, "Too many consecutive blank lines.")
			d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, repl)}}
			d.Fixability = r.meta.Fixable
			out = append(out, d)
		}
		run = 0
		start = -1
	}
	for i, t := range ctx.Tokens {
		if t.Kind == lexer.Newline {
			if run == 0 {
				start = i
			}
			run++
			continue
		}
		if run > 0 {
			flush(i - 1)
		}
	}
	if run > 0 {
		flush(len(ctx.Tokens) - 1)
	}
	return out
}

func init() {
	Register(&lt05{meta: newMeta("LT05", "layout.long_lines", "layout", Formatting, Lines, diagnostic.NoFix, "Line is too long.", "ignore-comment-lines", "ignore-comment-clauses")})
	Register(&lt12{meta: newMeta("LT12", "layout.end_of_file", "layout", Formatting, File, diagnostic.Safe, "Files must end with a single trailing newline.")})
	Register(&lt13{meta: newMeta("LT13", "layout.start_of_file", "layout", Formatting, File, diagnostic.Safe, "Files must not begin with newlines or whitespace.")})
	Register(&lt15{meta: newMeta("LT15", "layout.newlines", "layout", Formatting, Lines, diagnostic.Safe, "Too many consecutive blank lines.", "maximum-empty-lines")})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
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
