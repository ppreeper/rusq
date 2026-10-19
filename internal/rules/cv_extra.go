package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// CV04 — consistent syntax for counting rows.
type cv04 struct{ meta Meta }

func (r *cv04) Meta() Meta { return r.meta }
func (r *cv04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	want := "*"
	if ctx.Settings.RuleBool(r.meta.Name, "prefer-count-1", false) || ctx.Settings.RuleBool(r.meta.Name, "prefer_count_1", false) {
		want = "1"
	}
	if ctx.Settings.RuleBool(r.meta.Name, "prefer-count-0", false) || ctx.Settings.RuleBool(r.meta.Name, "prefer_count_0", false) {
		want = "0"
	}
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword && t.Kind != lexer.Identifier {
			continue
		}
		if t.Upper() != "COUNT" {
			continue
		}
		open := nextSig(ctx.Tokens, i)
		if open < 0 || ctx.Tokens[open].Kind != lexer.LParen {
			continue
		}
		arg := nextSig(ctx.Tokens, open)
		if arg < 0 {
			continue
		}
		a := ctx.Tokens[arg]
		if a.Kind == lexer.Keyword && a.Upper() == "DISTINCT" {
			continue
		}
		got := a.Text
		if a.Kind == lexer.Operator && got == "*" {
			got = "*"
		} else if a.Kind != lexer.Number {
			continue
		}
		if got != want {
			d := diag(r.meta, a.Span, "Use COUNT("+want+") instead of COUNT("+got+").")
			d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(a.Span, want)}}
			d.Fixability = r.meta.Fixable
			out = append(out, d)
		}
	}
	return out
}

// CV07 — top-level statements should not be wrapped in brackets.
type cv07 struct{ meta Meta }

func (r *cv07) Meta() Meta { return r.meta }
func (r *cv07) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		sigs := significantOf(st.Node.Leaves(nil))
		if len(sigs) > 0 && sigs[len(sigs)-1].Kind == lexer.Semicolon {
			sigs = sigs[:len(sigs)-1]
		}
		if len(sigs) < 2 || sigs[0].Kind != lexer.LParen || sigs[len(sigs)-1].Kind != lexer.RParen {
			continue
		}
		depth := 0
		enclosing := true
		for i, t := range sigs {
			switch t.Kind {
			case lexer.LParen:
				depth++
			case lexer.RParen:
				depth--
				if depth == 0 && i != len(sigs)-1 {
					enclosing = false
				}
			}
		}
		if !enclosing {
			continue
		}
		d := diag(r.meta, source.Range{Start: sigs[0].Span.Start, End: sigs[len(sigs)-1].Span.End}, "Top-level statement should not be wrapped in brackets.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{
			edit(sigs[0].Span, ""),
			edit(sigs[len(sigs)-1].Span, ""),
		}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// CV10 — consistent quotes for string literals.
type cv10 struct{ meta Meta }

func (r *cv10) Meta() Meta { return r.meta }
func (r *cv10) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	supportsDouble := dialectHasStringQuote(ctx, '"')
	policy := ctx.Settings.RuleString(r.meta.Name, "preferred-quoted-literal-style", "consistent")
	single, double := 0, 0
	for _, t := range ctx.Tokens {
		if t.Kind != lexer.String || len(t.Text) < 2 {
			continue
		}
		switch t.Text[0] {
		case '\'':
			single++
		case '"':
			if supportsDouble {
				double++
			}
		}
	}
	want := byte(0)
	switch policy {
	case "single", "single_quotes", "single-quotes":
		want = '\''
	case "double", "double_quotes", "double-quotes":
		if supportsDouble {
			want = '"'
		}
	default:
		if single > double {
			want = '\''
		} else if double > single {
			want = '"'
		}
	}
	if want == 0 {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, t := range ctx.Tokens {
		if t.Kind != lexer.String || len(t.Text) < 2 {
			continue
		}
		got := t.Text[0]
		if got == want {
			continue
		}
		if got == '"' && !supportsDouble {
			continue
		}
		repl := requote(t.Text, want)
		d := diag(r.meta, t.Span, "Use consistent quote style for string literals.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(t.Span, repl)}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

func dialectHasStringQuote(ctx *lint.Context, q byte) bool {
	for _, sq := range ctx.Dialect.StrQuote {
		if sq.Open == q {
			return true
		}
	}
	return false
}

func requote(s string, want byte) string {
	if len(s) < 2 {
		return s
	}
	inner := s[1 : len(s)-1]
	if want == '\'' {
		inner = strings.ReplaceAll(inner, `\`, `\\`)
		inner = strings.ReplaceAll(inner, "'", "''")
		return "'" + inner + "'"
	}
	inner = strings.ReplaceAll(inner, `"`, `""`)
	return `"` + inner + `"`
}

// CV11 — consistent type-casting style.
type cv11 struct{ meta Meta }

func (r *cv11) Meta() Meta { return r.meta }
func (r *cv11) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var casts, shorthands []lexer.Token
	for i, t := range ctx.Tokens {
		if t.Kind == lexer.Operator && t.Text == "::" {
			shorthands = append(shorthands, t)
		}
		if (t.Kind == lexer.Keyword || t.Kind == lexer.Identifier) && t.Upper() == "CAST" {
			n := nextSig(ctx.Tokens, i)
			if n >= 0 && ctx.Tokens[n].Kind == lexer.LParen {
				casts = append(casts, t)
			}
		}
	}
	if len(casts) == 0 && len(shorthands) == 0 {
		return nil
	}
	policy := ctx.Settings.RuleString(r.meta.Name, "preferred-type-casting-style", "consistent")
	want := policy
	if want == "consistent" {
		want = "cast"
		if len(shorthands) > len(casts) {
			want = "shorthand"
		}
	}
	var out []diagnostic.Diagnostic
	if want == "shorthand" {
		for _, t := range casts {
			out = append(out, diag(r.meta, t.Span, "Use :: shorthand for type casts."))
		}
	} else {
		for _, t := range shorthands {
			out = append(out, diag(r.meta, t.Span, "Use CAST(...) for type casts."))
		}
	}
	return out
}

// CV12 — use JOIN ... ON rather than WHERE for join conditions.
type cv12 struct{ meta Meta }

func (r *cv12) Meta() Meta { return r.meta }
func (r *cv12) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || len(st.Select.Joins) == 0 {
			continue
		}
		known := tableNames(st.Select)
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.WhereClause {
				continue
			}
			toks := c.Tokens
			for i := 0; i+6 < len(toks); i++ {
				if !isNameToken(toks[i].Kind) || toks[i+1].Kind != lexer.Dot ||
					!isNameToken(toks[i+2].Kind) || toks[i+3].Kind != lexer.Operator || toks[i+3].Text != "=" ||
					!isNameToken(toks[i+4].Kind) || toks[i+5].Kind != lexer.Dot || !isNameToken(toks[i+6].Kind) {
					continue
				}
				q1, q2 := unquote(toks[i].Text), unquote(toks[i+4].Text)
				if q1 != q2 && known[q1] && known[q2] {
					out = append(out, diag(r.meta, toks[i].Span, "Join condition should be in the JOIN ... ON clause."))
					break
				}
			}
		}
	}
	return out
}

// CV13 — final SELECT of a CTE model should be SELECT * FROM ...
type cv13 struct{ meta Meta }

func (r *cv13) Meta() Meta { return r.meta }
func (r *cv13) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if !ctx.Settings.RuleBool(r.meta.Name, "force-enable", false) {
		return nil
	}
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || st.Select.With == nil {
			continue
		}
		var last *ast.Clause
		for _, c := range st.Select.Clauses {
			if c.Kind == tree.SelectClause {
				last = c
			}
		}
		if last == nil || clauseHasStar(last) {
			continue
		}
		out = append(out, diag(r.meta, last.Node.Span(), "The final SELECT of a CTE model should be SELECT * FROM ..."))
	}
	return out
}

func clauseHasStar(c *ast.Clause) bool {
	for _, t := range c.Tokens {
		if t.Kind == lexer.Operator && t.Text == "*" {
			return true
		}
	}
	return false
}

func tableNames(s *ast.Select) map[string]bool {
	m := map[string]bool{}
	for _, tr := range allTables(s) {
		if tr.Alias != "" {
			m[strings.ToLower(tr.Alias)] = true
		}
		for _, p := range tr.Parts {
			m[strings.ToLower(p)] = true
		}
	}
	if s.With != nil {
		for _, c := range s.With.Ctes {
			m[strings.ToLower(c.Name)] = true
		}
	}
	return m
}

func init() {
	Register(&cv04{meta: newMeta("CV04", "convention.count_rows", "convention", Style, Token, diagnostic.Safe, "Consistent syntax for counting rows.", "prefer-count-1", "prefer-count-0")})
	Register(&cv07{meta: newMeta("CV07", "convention.statement_brackets", "convention", Style, Token, diagnostic.Safe, "Top-level statements should not be wrapped in brackets.")})
	Register(&cv10{meta: newMeta("CV10", "convention.quoted_literals", "convention", Style, Token, diagnostic.Safe, "Consistent preferred quotes for quoted literals.", "preferred-quoted-literal-style")})
	Register(&cv11{meta: newMeta("CV11", "convention.casting_style", "convention", Style, Token, diagnostic.NoFix, "Enforce consistent type casting style.", "preferred-type-casting-style")})
	Register(&cv12{meta: newMeta("CV12", "convention.join_condition", "convention", Style, Tree, diagnostic.NoFix, "Use JOIN ... ON instead of WHERE for join conditions.", "force-enable")})
	Register(&cv13{meta: newMeta("CV13", "convention.last_select_star", "convention", Style, Tree, diagnostic.NoFix, "Final SELECT of a CTE model should be SELECT * FROM ...", "force-enable")})
}
