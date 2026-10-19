package linter

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/rules"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/suppress"
)

func (l *Linter) applySuppressions(ds []diagnostic.Diagnostic, dirs []suppress.Directive, tokens []lexer.Token, loc *source.Locator) ([]diagnostic.Diagnostic, []suppress.Directive) {
	if len(dirs) == 0 {
		return ds, dirs
	}
	// File-level ignore.
	for i := range dirs {
		if dirs[i].Kind == suppress.FileIgnore {
			if len(ds) > 0 {
				dirs[i].Used = true
			}
			return nil, dirs
		}
	}
	skipEnd := make([]int, len(dirs))
	for i := range dirs {
		skipEnd[i] = -1
		if dirs[i].Kind != suppress.Skip {
			continue
		}
		end := loc.LineCount()
		for _, t := range tokens {
			if t.Kind == lexer.Semicolon && t.Span.Start > dirs[i].Span.Start {
				end = loc.Position(t.Span.End).Line
				break
			}
		}
		skipEnd[i] = end
	}

	var kept []diagnostic.Diagnostic
	for _, d := range ds {
		line := loc.Position(d.Span.Start).Line
		if !l.suppressed(d, line, dirs, skipEnd, loc) {
			kept = append(kept, d)
		}
	}
	return kept, dirs
}

func (l *Linter) suppressed(d diagnostic.Diagnostic, line int, dirs []suppress.Directive, skipEnd []int, loc *source.Locator) bool {
	// Line directives: ignore on the same line.
	for i := range dirs {
		if dirs[i].Kind == suppress.Ignore && dirs[i].Line == line && dirs[i].MatchesCode(d.Code) {
			dirs[i].Used = true
			return true
		}
	}
	// Skip ranges.
	for i := range dirs {
		if dirs[i].Kind == suppress.Skip && skipEnd[i] >= 0 && line >= dirs[i].Line && line <= skipEnd[i] && dirs[i].MatchesCode(d.Code) {
			dirs[i].Used = true
			return true
		}
	}
	// Disable/enable ranges.
	all := false
	codes := map[string]bool{}
	var active []int
	for i := range dirs {
		if dirs[i].Line > line {
			break
		}
		switch dirs[i].Kind {
		case suppress.Disable:
			if dirs[i].All {
				all = true
			}
			for _, c := range dirs[i].Codes {
				codes[strings.ToUpper(c)] = true
			}
			active = append(active, i)
		case suppress.Enable:
			if dirs[i].All {
				all = false
				codes = map[string]bool{}
				active = nil
			} else {
				for _, c := range dirs[i].Codes {
					delete(codes, strings.ToUpper(c))
				}
			}
		}
	}
	if all {
		for _, i := range active {
			dirs[i].Used = true
		}
		return true
	}
	if codes[strings.ToUpper(d.Code)] {
		for _, i := range active {
			dirs[i].Used = true
		}
		return true
	}
	return false
}

func (l *Linter) emitUnusedSuppressions(res *Result, dirs []suppress.Directive) {
	m := rules.Meta{
		Code:     "RUS102",
		Name:     "rusq.unused",
		Family:   "rusq",
		Category: rules.Style,
		Severity: diagnostic.Warning,
	}
	if !l.Settings.IsSelected(m.Code, m.Name, m.Family, string(m.Category), false) {
		return
	}
	for _, d := range dirs {
		switch d.Kind {
		case suppress.Ignore, suppress.Disable, suppress.FileIgnore, suppress.Skip:
		default:
			continue
		}
		if d.Used {
			continue
		}
		res.Diagnostics = append(res.Diagnostics, diagnostic.Diagnostic{
			Code:     m.Code,
			Name:     m.Name,
			Severity: m.Severity,
			Span:     d.Span,
			Message:  "Suppression directive is unused.",
		})
	}
}
