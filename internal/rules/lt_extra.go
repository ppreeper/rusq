package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// LT01 — inappropriate spacing around punctuation.
type lt01 struct{ meta Meta }

func (r *lt01) Meta() Meta { return r.meta }
func (r *lt01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	toks := ctx.Tokens
	for i, t := range toks {
		switch t.Kind {
		case lexer.Comma:
			if i > 0 && toks[i-1].Kind == lexer.Whitespace && !firstOnLine(toks, i) {
				ws := toks[i-1].Span
				d := diag(r.meta, ws, "Unexpected whitespace before comma.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(ws, "")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
			if i+1 < len(toks) && toks[i+1].Kind != lexer.Whitespace && toks[i+1].Kind != lexer.Newline && toks[i+1].Kind != lexer.EOF && toks[i+1].Kind != lexer.RParen {
				at := source.NewRangeAt(t.Span.End)
				d := diag(r.meta, at, "Expected a space after comma.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(at, " ")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
		case lexer.Dot:
			if i > 0 && toks[i-1].Kind == lexer.Whitespace {
				ws := toks[i-1].Span
				d := diag(r.meta, ws, "Unexpected whitespace before dot.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(ws, "")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
			if i+1 < len(toks) && toks[i+1].Kind == lexer.Whitespace {
				ws := toks[i+1].Span
				d := diag(r.meta, ws, "Unexpected whitespace after dot.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(ws, "")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
		case lexer.Semicolon:
			if i > 0 && toks[i-1].Kind == lexer.Whitespace {
				ws := toks[i-1].Span
				d := diag(r.meta, ws, "Unexpected whitespace before semicolon.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(ws, "")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
		}
	}
	return out
}

// firstOnLine reports whether the token at i is the first significant token on
// its line, ignoring indentation and comments. A leading comma (indented on its
// own line) is the first token of its line, so the whitespace before it is
// indentation rather than "whitespace before a comma".
func firstOnLine(toks []lexer.Token, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch toks[j].Kind {
		case lexer.Newline:
			return true
		case lexer.Whitespace, lexer.Comment:
			continue
		default:
			return false
		}
	}
	return true
}

// LT02 — indentation.
type lt02 struct{ meta Meta }

func (r *lt02) Meta() Meta { return r.meta }
func (r *lt02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.Settings.Format.IndentUnit != "space" {
		return nil
	}
	width := ctx.Settings.Format.IndentWidth
	if width <= 0 {
		width = 4
	}
	var out []diagnostic.Diagnostic
	for _, line := range ctx.Lines.Physical {
		if strings.TrimSpace(line.Text) == "" || strings.HasPrefix(strings.TrimSpace(line.Text), "--") {
			continue
		}
		lead := 0
		hasTab := false
		for lead < len(line.Text) && (line.Text[lead] == ' ' || line.Text[lead] == '\t') {
			if line.Text[lead] == '\t' {
				hasTab = true
			}
			lead++
		}
		if hasTab {
			out = append(out, diag(r.meta, source.Range{Start: line.Start, End: line.Start + lead}, "Indentation uses tabs; spaces are configured."))
			continue
		}
		if lead%width != 0 {
			out = append(out, diag(r.meta, source.Range{Start: line.Start, End: line.Start + lead}, "Indentation is not a multiple of the indent width."))
		}
	}
	return out
}

// LT03 — operator line position.
type lt03 struct{ meta Meta }

func (r *lt03) Meta() Meta { return r.meta }
func (r *lt03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	policy := ctx.Settings.RuleString(r.meta.Name, "line-position", "")
	if policy == "" {
		policy = ctx.Settings.Format.TypeLinePosition("operator", ctx.Settings.Format.LinePosition)
	}
	leading := policy == "leading"
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Operator || !isBinaryOperator(t.Text) {
			continue
		}
		prev := prevSig(ctx.Tokens, i)
		next := nextSig(ctx.Tokens, i)
		firstOnLine := prev < 0 || ctx.Tokens[prev].Line != t.Line
		lastOnLine := next < 0 || ctx.Tokens[next].Line != t.Line
		if leading && lastOnLine && !firstOnLine {
			out = append(out, diag(r.meta, t.Span, "Operator should be at the start of a line."))
		}
		if !leading && firstOnLine && !lastOnLine {
			out = append(out, diag(r.meta, t.Span, "Operator should be at the end of a line."))
		}
	}
	return out
}

func isBinaryOperator(s string) bool {
	switch s {
	case "+", "-", "*", "/", "%", "=", "<", ">", "<=", ">=", "<>", "!=", "||", "&&", "AND", "OR":
		return true
	}
	return false
}

// LT04 — comma line position.
type lt04 struct{ meta Meta }

func (r *lt04) Meta() Meta { return r.meta }
func (r *lt04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	policy := ctx.Settings.RuleString(r.meta.Name, "line-position", "")
	if policy == "" {
		fallback := ctx.Settings.Format.CommaStyle
		if fallback == "" {
			fallback = ctx.Settings.Format.LinePosition
		}
		policy = ctx.Settings.Format.TypeLinePosition("comma", fallback)
	}
	leading := policy == "leading"
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Comma {
			continue
		}
		prev := prevSig(ctx.Tokens, i)
		next := nextSig(ctx.Tokens, i)
		firstOnLine := prev < 0 || ctx.Tokens[prev].Line != t.Line
		lastOnLine := next < 0 || ctx.Tokens[next].Line != t.Line
		if leading && lastOnLine && !firstOnLine {
			out = append(out, diag(r.meta, t.Span, "Comma should be at the start of a line."))
		}
		if !leading && firstOnLine && !lastOnLine {
			out = append(out, diag(r.meta, t.Span, "Comma should be at the end of a line."))
		}
	}
	return out
}

// LT06 — no space between a function name and its parenthesis.
type lt06 struct{ meta Meta }

func (r *lt06) Meta() Meta { return r.meta }
func (r *lt06) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Identifier && !isFunctionKeyword(t) {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n <= i+1 || n < 0 || ctx.Tokens[n].Kind != lexer.LParen {
			continue
		}
		span := source.Range{Start: t.Span.End, End: ctx.Tokens[n].Span.Start}
		d := diag(r.meta, span, "Function name should be immediately followed by a parenthesis.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, "")}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// LT07 — CTE closing bracket on a new line.
type lt07 struct{ meta Meta }

func (r *lt07) Meta() Meta { return r.meta }
func (r *lt07) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || st.Select.With == nil {
			continue
		}
		toks := st.Select.With.Node.Leaves(nil)
		depth := 0
		for i, t := range toks {
			switch t.Kind {
			case lexer.LParen:
				depth++
			case lexer.RParen:
				if depth == 1 {
					prev := prevSigPtr(toks, i)
					if prev >= 0 && toks[prev].Line == t.Line {
						out = append(out, diag(r.meta, t.Span, "CTE closing bracket should be on a new line."))
					}
				}
				if depth > 0 {
					depth--
				}
			}
		}
	}
	return out
}

// LT08 — blank line after a CTE closing bracket.
type lt08 struct{ meta Meta }

func (r *lt08) Meta() Meta { return r.meta }
func (r *lt08) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || st.Select.With == nil {
			continue
		}
		// Scan the whole statement so the lookahead past a CTE's closing
		// bracket can see the main SELECT (which lives outside the WITH node).
		all := st.Node.Leaves(nil)
		start := indexToken(all, st.Select.With.Node.FirstToken())
		if start < 0 {
			continue
		}
		end := indexToken(all, st.Select.With.Node.LastToken())
		if end < 0 {
			end = len(all) - 1
		}
		depth := 0
		for i := start; i <= end && i < len(all); i++ {
			t := all[i]
			switch t.Kind {
			case lexer.LParen:
				depth++
			case lexer.RParen:
				if depth == 1 {
					n := nextSigPtr(all, i)
					if n >= 0 && (all[n].Kind == lexer.Comma || all[n].Upper() == "SELECT") {
						newlines := 0
						for j := i + 1; j < n; j++ {
							if all[j].Kind == lexer.Newline {
								newlines++
							}
						}
						if newlines < 2 && all[n].Kind != lexer.Comma {
							out = append(out, diag(r.meta, t.Span, "Expected a blank line after the CTE closing bracket."))
						}
					}
				}
				if depth > 0 {
					depth--
				}
			}
		}
	}
	return out
}

// LT09 — select targets on their own lines.
type lt09 struct{ meta Meta }

func (r *lt09) Meta() Meta { return r.meta }
func (r *lt09) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	policy := strings.ReplaceAll(ctx.Settings.RuleString(r.meta.Name, "single-target-policy", ctx.Settings.Format.SingleTargetPolicy), "-", "_")
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.SelectClause {
				continue
			}
			n := countSelectTargets(c)
			if len(c.Tokens) < 2 {
				continue
			}
			selectTok := c.Tokens[0]
			firstTarget := c.Tokens[1]
			if n > 1 {
				if firstTarget.Line == selectTok.Line {
					out = append(out, diag(r.meta, firstTarget.Span, "Select targets should each be on a new line."))
				}
			} else if policy == "new_line" && firstTarget.Line == selectTok.Line {
				out = append(out, diag(r.meta, firstTarget.Span, "Select target should be on a new line."))
			}
		}
	}
	return out
}

// LT10 — SELECT modifiers on the same line as SELECT.
type lt10 struct{ meta Meta }

func (r *lt10) Meta() Meta { return r.meta }
func (r *lt10) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.SelectClause || len(c.Tokens) < 2 {
				continue
			}
			sel := c.Tokens[0]
			mod := c.Tokens[1]
			switch mod.Upper() {
			case "DISTINCT", "ALL", "DISTINCTROW":
				if mod.Line != sel.Line {
					out = append(out, diag(r.meta, mod.Span, "SELECT modifier should be on the same line as SELECT."))
				}
			}
		}
	}
	return out
}

// LT11 — set operators surrounded by newlines.
type lt11 struct{ meta Meta }

func (r *lt11) Meta() Meta { return r.meta }
func (r *lt11) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.SetOperation || len(c.Tokens) == 0 {
				continue
			}
			tok := c.Tokens[0]
			idx := -1
			for i := range ctx.Tokens {
				if ctx.Tokens[i].Span == tok.Span {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			before := false
			for j := idx - 1; j >= 0; j-- {
				if ctx.Tokens[j].Kind == lexer.Newline {
					before = true
					break
				}
				if ctx.Tokens[j].Kind != lexer.Whitespace {
					break
				}
			}
			after := idx+1 < len(ctx.Tokens) && ctx.Tokens[idx+1].Kind == lexer.Newline
			if !before || !after {
				out = append(out, diag(r.meta, tok.Span, "Set operators should be surrounded by newlines."))
			}
		}
	}
	return out
}

// LT14 — keyword clauses before/after newlines.
type lt14 struct{ meta Meta }

func (r *lt14) Meta() Meta { return r.meta }
func (r *lt14) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			switch c.Kind {
			case tree.FromClause, tree.WhereClause, tree.GroupByClause, tree.HavingClause,
				tree.OrderByClause, tree.LimitClause, tree.JoinClause:
			default:
				continue
			}
			if len(c.Tokens) == 0 {
				continue
			}
			tok := c.Tokens[0]
			idx := -1
			for i := range ctx.Tokens {
				if ctx.Tokens[i].Span == tok.Span {
					idx = i
					break
				}
			}
			if idx <= 0 {
				continue
			}
			prev := prevSig(ctx.Tokens, idx)
			kwPolicy := ctx.Settings.Format.TypeLinePosition("keyword", ctx.Settings.Format.KeywordLinePosition)
			if strings.EqualFold(kwPolicy, "trailing") {
				next := nextSig(ctx.Tokens, idx)
				if next >= 0 && ctx.Tokens[next].Line == tok.Line {
					out = append(out, diag(r.meta, tok.Span, "Keyword clause should end its line (trailing position)."))
				}
				continue
			}
			if prev >= 0 && ctx.Tokens[prev].Line == tok.Line {
				out = append(out, diag(r.meta, tok.Span, "Keyword clause should start on a new line."))
			}
		}
	}
	return out
}

func init() {
	Register(&lt01{meta: newMeta("LT01", "layout.spacing", "layout", Formatting, Lines, diagnostic.Safe, "Inappropriate spacing.")})
	Register(&lt02{meta: newMeta("LT02", "layout.indent", "layout", Formatting, Lines, diagnostic.NoFix, "Incorrect indentation.")})
	Register(&lt03{meta: newMeta("LT03", "layout.operators", "layout", Formatting, Lines, diagnostic.NoFix, "Operators should be consistently placed.", "line-position")})
	Register(&lt04{meta: newMeta("LT04", "layout.commas", "layout", Formatting, Lines, diagnostic.NoFix, "Leading/trailing comma enforcement.", "line-position")})
	Register(&lt06{meta: newMeta("LT06", "layout.functions", "layout", Formatting, Lines, diagnostic.Safe, "Function name not immediately followed by a parenthesis.")})
	Register(&lt07{meta: newMeta("LT07", "layout.cte_bracket", "layout", Formatting, Tree, diagnostic.NoFix, "WITH clause closing bracket should be on a new line.")})
	Register(&lt08{meta: newMeta("LT08", "layout.cte_newline", "layout", Formatting, Tree, diagnostic.NoFix, "Blank line expected after a CTE closing bracket.")})
	Register(&lt09{meta: newMeta("LT09", "layout.select_targets", "layout", Formatting, Tree, diagnostic.NoFix, "Select targets should be on a new line unless there is only one.", "single-target-policy")})
	Register(&lt10{meta: newMeta("LT10", "layout.select_modifiers", "layout", Formatting, Tree, diagnostic.NoFix, "SELECT modifiers should be on the same line as SELECT.")})
	Register(&lt11{meta: newMeta("LT11", "layout.set_operators", "layout", Formatting, Tree, diagnostic.NoFix, "Set operators should be surrounded by newlines.")})
	Register(&lt14{meta: newMeta("LT14", "layout.keyword_newline", "layout", Formatting, Tree, diagnostic.NoFix, "Keyword clauses should be surrounded by newlines.")})
}
