package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
)

// ST01 — redundant ELSE NULL.
type st01 struct{ meta Meta }

func (r *st01) Meta() Meta { return r.meta }
func (r *st01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || t.Upper() != "ELSE" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n < 0 || ctx.Tokens[n].Kind != lexer.Keyword || ctx.Tokens[n].Upper() != "NULL" {
			continue
		}
		end := ctx.Tokens[n].Span.End
		span := sourceRange(t.Span.Start, end)
		d := diag(r.meta, span, "ELSE NULL is redundant.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(span, "")}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// ST03 — unused CTE.
type st03 struct{ meta Meta }

func (r *st03) Meta() Meta { return r.meta }
func (r *st03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || st.Select.With == nil {
			continue
		}
		sigs := significantOf(st.Node.Leaves(nil))
		for _, cte := range st.Select.With.Ctes {
			count := 0
			var nameSpan = sourceRange(0, 0)
			for _, t := range sigs {
				if isNameToken(t.Kind) && strings.EqualFold(unquote(t.Text), cte.Name) {
					count++
					if nameSpan.Start == 0 && nameSpan.End == 0 {
						nameSpan = t.Span
					}
				}
			}
			if count <= 1 {
				// Cross-file extension (SPEC §8.3): a CTE is not unused when
				// another file in the project references it.
				if ctx.Index != nil && ctx.Index.ReferencedElsewhere(ctx.Source.Path, cte.Name) {
					continue
				}
				out = append(out, diag(r.meta, nameSpan, "CTE "+cte.Name+" is defined but not used."))
			}
		}
	}
	return out
}

// ST08 — DISTINCT with parentheses.
type st08 struct{ meta Meta }

func (r *st08) Meta() Meta { return r.meta }
func (r *st08) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || t.Upper() != "DISTINCT" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n >= 0 && ctx.Tokens[n].Kind == lexer.LParen {
			out = append(out, diag(r.meta, t.Span, "DISTINCT should not be used with parentheses."))
		}
	}
	return out
}

// ST12 — consecutive semicolons.
type st12 struct{ meta Meta }

func (r *st12) Meta() Meta { return r.meta }
func (r *st12) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	sigs := ctx.Significant()
	for i, t := range sigs {
		if t.Kind != lexer.Semicolon {
			continue
		}
		if n := nextSig(sigs, i); n >= 0 && sigs[n].Kind == lexer.Semicolon {
			d := diag(r.meta, sigs[n].Span, "Consecutive semicolons detected.")
			d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(sigs[n].Span, "")}}
			d.Fixability = r.meta.Fixable
			out = append(out, d)
		}
	}
	return out
}

func init() {
	Register(&st01{meta: newMeta("ST01", "structure.else_null", "structure", Style, Token, diagnostic.Safe, "Do not specify ELSE NULL in a CASE WHEN expression.")})
	Register(&st03{meta: newMeta("ST03", "structure.unused_cte", "structure", Suspicious, Tree, diagnostic.NoFix, "CTE defined but not used.")})
	Register(&st08{meta: newMeta("ST08", "structure.distinct", "structure", Style, Token, diagnostic.NoFix, "DISTINCT used with parentheses.")})
	Register(&st12{meta: newMeta("ST12", "structure.consecutive_semicolons", "structure", Suspicious, Token, diagnostic.Safe, "Consecutive semicolons detected.")})
}
