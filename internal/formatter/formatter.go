// Package formatter implements an opinionated, idempotent SQL formatter built
// on a Wadler/Prettier-style document IR.
//
// The formatter is layout-only: it never drops, reorders, or rewrites
// significant tokens. It reads the lossless CST and re-emits every token in
// order, replacing trivia with computed layout. That guarantees the significant
// token stream is preserved (no semantic change) and that formatting is
// idempotent.
package formatter

import (
	"bytes"
	"strings"

	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// Formatter formats SQL.
type Formatter struct {
	Settings settings.Settings
	// StartLine and EndLine limit formatting to a 1-based line range. Zero
	// means the whole file.
	StartLine int
	EndLine   int
}

// New returns a formatter for the settings.
func New(s settings.Settings) *Formatter { return &Formatter{Settings: s} }

// FormatSource lexes/parses and formats src, returning the new bytes.
func (f *Formatter) FormatSource(src source.Text) ([]byte, error) {
	return f.FormatBytes(src.Bytes), nil
}

// Format formats a tree, falling back to the raw source.
func (f *Formatter) Format(t *tree.Tree, raw []byte) []byte {
	if t != nil && len(t.Errors) == 0 {
		if d := newBuilder(f.Settings).buildTree(t); d != nil {
			return f.finish(render(d, f.lineWidth(), f.indentUnit()))
		}
	}
	return f.FormatBytes(raw)
}

// FormatBytes formats raw source bytes.
//
// When the file parses cleanly the document formatter runs. When it does not
// (parse errors), only whitespace-level normalisation is applied so that the
// unparseable region keeps its meaning and tokens.
func (f *Formatter) FormatBytes(in []byte) []byte {
	if len(in) == 0 {
		return in
	}
	// Range formatting uses the conservative line-level path.
	if f.StartLine > 0 || f.EndLine > 0 {
		return f.lineNormalize(in)
	}
	src := source.Text{Bytes: in}
	d := f.Settings.Dialect
	toks, _ := lexer.Lex(src, d)
	p := parser.New()
	if f.Settings.MaxParseDepth > 0 {
		p.MaxDepth = f.Settings.MaxParseDepth
	}
	if f.Settings.MaxParseNodes > 0 {
		p.MaxNodes = f.Settings.MaxParseNodes
	}
	t, errs := p.Parse(toks, d)
	if t != nil && len(errs) == 0 && len(t.Errors) == 0 {
		if doc := newBuilder(f.Settings).buildTree(t); doc != nil {
			return f.finish(render(doc, f.lineWidth(), f.indentUnit()))
		}
	}
	return f.lineNormalize(in)
}

func (f *Formatter) lineWidth() int {
	if f.Settings.Format.LineLength > 0 {
		return f.Settings.Format.LineLength
	}
	if f.Settings.LineLength > 0 {
		return f.Settings.LineLength
	}
	return 88
}

func (f *Formatter) indentUnit() string {
	if strings.EqualFold(f.Settings.Format.IndentUnit, "tab") {
		return "\t"
	}
	w := f.Settings.Format.IndentWidth
	if w <= 0 {
		w = 4
	}
	return strings.Repeat(" ", w)
}

// finish trims trailing blank lines and applies the end-of-file newline policy.
func (f *Formatter) finish(s string) []byte {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}
	out := strings.Join(lines, "\n")
	if f.Settings.Format.NewlineAtEOF {
		out += "\n"
	}
	return []byte(out)
}

// lineNormalize is the conservative, whitespace-only fallback.
func (f *Formatter) lineNormalize(in []byte) []byte {
	if len(in) == 0 {
		return in
	}
	s := strings.ReplaceAll(string(in), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")

	start, end := 0, len(lines)
	if f.StartLine > 0 {
		start = clamp(f.StartLine-1, 0, len(lines))
	}
	if f.EndLine > 0 {
		end = clamp(f.EndLine, start, len(lines))
	}

	maxBlank := f.Settings.Format.MaxBlankLines
	if maxBlank < 0 {
		maxBlank = 0
	}

	out := make([]string, 0, len(lines))
	blank := 0
	for i, line := range lines {
		if i >= start && i < end {
			line = strings.TrimRight(line, " \t")
		}
		if strings.TrimSpace(line) == "" {
			if i < start || i >= end {
				out = append(out, line)
				continue
			}
			blank++
			if blank > maxBlank {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, line)
	}

	if f.StartLine == 0 {
		for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
			out = out[1:]
		}
	}
	if f.Settings.Format.NewlineAtEOF {
		for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		return []byte(strings.Join(out, "\n") + "\n")
	}
	return []byte(strings.Join(out, "\n"))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Equal reports whether the formatted output differs from the input.
func Equal(a, b []byte) bool { return bytes.Equal(a, b) }
