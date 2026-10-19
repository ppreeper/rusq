package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

func allTables(s *ast.Select) []*ast.TableRef {
	if s == nil {
		return nil
	}
	out := append([]*ast.TableRef{}, s.Tables...)
	for _, j := range s.Joins {
		if j.Table != nil {
			out = append(out, j.Table)
		}
	}
	return out
}

func lastToken(toks []*lexer.Token) *lexer.Token {
	if len(toks) == 0 {
		return nil
	}
	return toks[len(toks)-1]
}

func findKeyword(toks []*lexer.Token, kw string) *lexer.Token {
	for _, t := range toks {
		if t.Kind == lexer.Keyword && t.Upper() == kw {
			return t
		}
	}
	return nil
}

// AL01/AL02 — explicit vs implicit aliasing.
type aliasStyle struct {
	meta   Meta
	tables bool
	key    string
}

func (r *aliasStyle) Meta() Meta { return r.meta }
func (r *aliasStyle) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	policy := ctx.Settings.RuleString(r.meta.Name, r.key, "explicit")
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		if r.tables {
			for _, tr := range allTables(st.Select) {
				out = append(out, aliasDiagnostics(r.meta, tr.Tokens, tr.Alias, tr.HasAS, policy)...)
			}
		} else {
			for _, col := range st.Select.Columns {
				out = append(out, aliasDiagnostics(r.meta, col.Tokens, col.Alias, col.HasAS, policy)...)
			}
		}
	}
	return out
}

func aliasDiagnostics(m Meta, toks []*lexer.Token, alias string, hasAS bool, policy string) []diagnostic.Diagnostic {
	if alias == "" {
		return nil
	}
	aliasTok := lastToken(toks)
	if aliasTok == nil {
		return nil
	}
	switch strings.ToLower(policy) {
	case "explicit":
		if hasAS {
			return nil
		}
		d := diag(m, source.NewRangeAt(aliasTok.Span.Start), "Use explicit AS for aliases.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(aliasTok.Span.Start), "AS ")}}
		d.Fixability = m.Fixable
		return []diagnostic.Diagnostic{d}
	case "implicit":
		if !hasAS {
			return nil
		}
		asTok := findKeyword(toks, "AS")
		if asTok == nil {
			return nil
		}
		start := asTok.Span.Start
		if i := indexToken(toks, asTok); i > 0 {
			start = toks[i-1].Span.End
		}
		span := source.Range{Start: start, End: aliasTok.Span.End}
		d := diag(m, span, "Use implicit aliasing (omit AS).")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, " "+alias)}}
		d.Fixability = m.Fixable
		return []diagnostic.Diagnostic{d}
	}
	return nil
}

func indexToken(toks []*lexer.Token, target *lexer.Token) int {
	for i, t := range toks {
		if t == target {
			return i
		}
	}
	return -1
}

// AL04 — unique table aliases.
type al04 struct{ meta Meta }

func (r *al04) Meta() Meta { return r.meta }
func (r *al04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		seen := map[string]bool{}
		for _, tr := range allTables(st.Select) {
			if tr.Alias == "" {
				continue
			}
			key := strings.ToLower(tr.Alias)
			if seen[key] {
				if tok := lastToken(tr.Tokens); tok != nil {
					out = append(out, diag(r.meta, tok.Span, "Duplicate table alias "+tr.Alias+"."))
				}
				continue
			}
			seen[key] = true
		}
	}
	return out
}

// AL05 — unused table aliases.
type al05 struct{ meta Meta }

func (r *al05) Meta() Meta { return r.meta }
func (r *al05) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		sigs := significantOf(st.Node.Leaves(nil))
		for _, tr := range allTables(st.Select) {
			if tr.Alias == "" {
				continue
			}
			n := 0
			for _, t := range sigs {
				if isNameToken(t.Kind) && strings.EqualFold(unquote(t.Text), tr.Alias) {
					n++
				}
			}
			if n <= 1 {
				if tok := lastToken(tr.Tokens); tok != nil {
					out = append(out, diag(r.meta, tok.Span, "Alias "+tr.Alias+" is never used."))
				}
			}
		}
	}
	return out
}

// AL08 — unique column aliases.
type al08 struct{ meta Meta }

func (r *al08) Meta() Meta { return r.meta }
func (r *al08) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		seen := map[string]bool{}
		for _, col := range st.Select.Columns {
			if col.Alias == "" {
				continue
			}
			key := strings.ToLower(col.Alias)
			if seen[key] {
				if tok := lastToken(col.Tokens); tok != nil {
					out = append(out, diag(r.meta, tok.Span, "Duplicate column alias "+col.Alias+"."))
				}
				continue
			}
			seen[key] = true
		}
	}
	return out
}

// AL09 — a column should not alias to itself.
type al09 struct{ meta Meta }

func (r *al09) Meta() Meta { return r.meta }
func (r *al09) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, col := range st.Select.Columns {
			if col.Alias == "" || col.Star {
				continue
			}
			body, _, _ := splitAliasTokens(col.Tokens)
			if len(body) == 0 || len(body) > 3 {
				continue
			}
			expr := unquote(strings.TrimSpace(joinTokenText(body)))
			if strings.EqualFold(expr, col.Alias) {
				aliasTok := lastToken(col.Tokens)
				span := source.Range{Start: body[len(body)-1].Span.End, End: aliasTok.Span.End}
				d := diag(r.meta, span, "Column is aliased to itself.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, "")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
		}
	}
	return out
}

// AL10 — derived tables must have an alias.
type al10 struct{ meta Meta }

func (r *al10) Meta() Meta { return r.meta }
func (r *al10) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, tr := range allTables(st.Select) {
			if tr.Subquery && tr.Alias == "" {
				if tok := lastToken(tr.Tokens); tok != nil {
					out = append(out, diag(r.meta, tok.Span, "Derived table must have an alias."))
				}
			}
		}
	}
	return out
}

func splitAliasTokens(toks []*lexer.Token) (body []*lexer.Token, alias string, hasAS bool) {
	depth := 0
	for i := len(toks) - 1; i >= 0; i-- {
		switch toks[i].Kind {
		case lexer.RParen:
			depth++
		case lexer.LParen:
			depth--
		case lexer.Keyword:
			if depth == 0 && toks[i].Upper() == "AS" && i+1 < len(toks) {
				return toks[:i], unquote(toks[i+1].Text), true
			}
		}
	}
	if len(toks) >= 2 && isNameToken(toks[len(toks)-1].Kind) {
		prev := toks[len(toks)-2]
		if prev.Kind != lexer.Dot && prev.Kind != lexer.Operator {
			if prev.Kind != lexer.Keyword || prev.Upper() == "END" {
				return toks[:len(toks)-1], unquote(toks[len(toks)-1].Text), false
			}
		}
	}
	return toks, "", false
}

func joinTokenText(toks []*lexer.Token) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(t.Text)
	}
	return b.String()
}

func significantOf(toks []*lexer.Token) []*lexer.Token {
	out := make([]*lexer.Token, 0, len(toks))
	for _, t := range toks {
		if t.IsSignificant() {
			out = append(out, t)
		}
	}
	return out
}

func init() {
	Register(&aliasStyle{meta: newMeta("AL01", "aliasing.table", "aliasing", Style, Tree, diagnostic.Safe, "Implicit/explicit aliasing of table.", "aliasing"), tables: true, key: "aliasing"})
	Register(&aliasStyle{meta: newMeta("AL02", "aliasing.column", "aliasing", Style, Tree, diagnostic.Safe, "Implicit/explicit aliasing of columns.", "aliasing"), key: "aliasing"})
	Register(&al04{meta: newMeta("AL04", "aliasing.unique.table", "aliasing", Correctness, Tree, diagnostic.NoFix, "Table aliases should be unique within each clause.")})
	Register(&al05{meta: newMeta("AL05", "aliasing.unused", "aliasing", Suspicious, Tree, diagnostic.NoFix, "Tables should not be aliased if that alias is not used.", "alias-case-check")})
	Register(&al08{meta: newMeta("AL08", "aliasing.unique.column", "aliasing", Correctness, Tree, diagnostic.NoFix, "Column aliases should be unique within each clause.")})
	Register(&al09{meta: newMeta("AL09", "aliasing.self_alias.column", "aliasing", Style, Tree, diagnostic.Safe, "Column aliases should not alias to itself.")})
	Register(&al10{meta: newMeta("AL10", "aliasing.required", "aliasing", Correctness, Tree, diagnostic.NoFix, "Derived tables must have an alias.")})
}
