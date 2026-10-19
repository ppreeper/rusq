package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
)

// AL03 — column expression without an alias.
type al03 struct{ meta Meta }

func (r *al03) Meta() Meta { return r.meta }
func (r *al03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	allowScalar := ctx.Settings.RuleBool(r.meta.Name, "allow-scalar", true)
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, col := range st.Select.Columns {
			if col.Star || col.Alias != "" || simpleColumnExpr(col.Tokens) {
				continue
			}
			if allowScalar && scalarExpr(col.Tokens) {
				continue
			}
			out = append(out, diag(r.meta, col.Tokens[0].Span, "Column expression should have an alias."))
		}
	}
	return out
}

// AL06 — alias length.
type al06 struct{ meta Meta }

func (r *al06) Meta() Meta { return r.meta }
func (r *al06) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	min := ctx.Settings.RuleInt(r.meta.Name, "min-alias-length", 0)
	max := ctx.Settings.RuleInt(r.meta.Name, "max-alias-length", 0)
	if min == 0 && max == 0 {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, tr := range allTables(st.Select) {
			if tr.Alias == "" {
				continue
			}
			n := len(tr.Alias)
			if (min > 0 && n < min) || (max > 0 && n > max) {
				if tok := lastToken(tr.Tokens); tok != nil {
					out = append(out, diag(r.meta, tok.Span, "Table alias length is outside the configured bounds."))
				}
			}
		}
	}
	return out
}

// AL07 — table aliases are forbidden.
type al07 struct{ meta Meta }

func (r *al07) Meta() Meta { return r.meta }
func (r *al07) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, tr := range allTables(st.Select) {
			if tr.Alias == "" {
				continue
			}
			if tok := lastToken(tr.Tokens); tok != nil {
				out = append(out, diag(r.meta, tok.Span, "Table aliases are forbidden."))
			}
		}
	}
	return out
}

func simpleColumnExpr(toks []*lexer.Token) bool {
	if len(toks) == 0 || len(toks)%2 == 0 {
		return false
	}
	for i, t := range toks {
		if i%2 == 0 {
			if !isNameToken(t.Kind) {
				return false
			}
		} else if t.Kind != lexer.Dot {
			return false
		}
	}
	return true
}

func scalarExpr(toks []*lexer.Token) bool {
	if len(toks) != 1 {
		return false
	}
	t := toks[0]
	if t.Kind == lexer.Number || t.Kind == lexer.String {
		return true
	}
	u := t.Upper()
	return u == "NULL" || u == "TRUE" || u == "FALSE"
}

func init() {
	Register(&al03{meta: newMeta("AL03", "aliasing.expression", "aliasing", Style, Tree, diagnostic.NoFix, "Column expression without alias; use explicit AS.", "allow-scalar")})
	Register(&al06{meta: newMeta("AL06", "aliasing.length", "aliasing", Pedantic, Tree, diagnostic.NoFix, "Enforce table alias lengths in FROM/JOIN.", "min-alias-length", "max-alias-length")})
	Register(&al07{meta: newMeta("AL07", "aliasing.forbid", "aliasing", Restriction, Tree, diagnostic.NoFix, "Avoid table aliases in FROM/JOIN.", "force-enable")})
}

var _ = strings.TrimSpace
