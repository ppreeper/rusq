package rules

import (
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// AM01 — DISTINCT with GROUP BY.
type am01 struct{ meta Meta }

func (r *am01) Meta() Meta { return r.meta }
func (r *am01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		hasDistinct, hasGroup := false, false
		for _, c := range st.Select.Clauses {
			switch c.Kind {
			case tree.SelectClause:
				for _, t := range c.Tokens {
					if t.Kind == lexer.Keyword && t.Upper() == "DISTINCT" {
						hasDistinct = true
					}
				}
			case tree.GroupByClause:
				hasGroup = true
			}
		}
		if hasDistinct && hasGroup {
			out = append(out, diag(r.meta, st.Select.Node.Span(), "DISTINCT is ambiguous with GROUP BY."))
		}
	}
	return out
}

// AM02 — UNION without ALL/DISTINCT.
type am02 struct{ meta Meta }

func (r *am02) Meta() Meta { return r.meta }
func (r *am02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	switch ctx.Dialect.Name {
	case "duckdb", "exasol", "postgres", "sparksql":
		return nil
	}
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || t.Upper() != "UNION" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n >= 0 {
			u := ctx.Tokens[n].Upper()
			if u == "ALL" || u == "DISTINCT" {
				continue
			}
		}
		d := diag(r.meta, t.Span, "UNION should specify DISTINCT or ALL.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(t.Span.End), " DISTINCT")}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// AM03 — ambiguous ORDER BY direction.
type am03 struct{ meta Meta }

func (r *am03) Meta() Meta { return r.meta }
func (r *am03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.OrderByClause {
				continue
			}
			items := splitAtCommas(c.Tokens)
			if len(items) < 2 {
				continue
			}
			for _, item := range items {
				if len(item) == 0 {
					continue
				}
				last := item[len(item)-1]
				if last.Kind == lexer.Keyword {
					u := last.Upper()
					if u == "ASC" || u == "DESC" || u == "FIRST" || u == "LAST" {
						continue
					}
				}
				d := diag(r.meta, last.Span, "Ambiguous ordering direction; specify ASC or DESC.")
				d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(last.Span.End), " ASC")}}
				d.Fixability = r.meta.Fixable
				out = append(out, d)
			}
		}
	}
	return out
}

// AM04 — unknown result column count.
type am04 struct{ meta Meta }

func (r *am04) Meta() Meta { return r.meta }
func (r *am04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		if st.Select.HasStar && len(st.Select.SetOps) > 0 {
			out = append(out, diag(r.meta, st.Select.Node.Span(), "Query produces an unknown number of result columns."))
		}
	}
	return out
}

// AM08 — implicit cross join.
type am08 struct{ meta Meta }

func (r *am08) Meta() Meta { return r.meta }
func (r *am08) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || len(st.Select.Tables) < 2 {
			continue
		}
		for _, tr := range st.Select.Tables[1:] {
			out = append(out, diag(r.meta, tr.Tokens[0].Span, "Implicit cross join detected; use explicit JOIN syntax."))
		}
	}
	return out
}

// AM09 — LIMIT/OFFSET without ORDER BY.
type am09 struct{ meta Meta }

func (r *am09) Meta() Meta { return r.meta }
func (r *am09) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		var limit, order *tree.Node
		for _, c := range st.Select.Clauses {
			switch c.Kind {
			case tree.LimitClause, tree.OffsetClause:
				if limit == nil {
					limit = c.Node
				}
			case tree.OrderByClause:
				order = c.Node
			}
		}
		if limit != nil && order == nil {
			out = append(out, diag(r.meta, limit.Span(), "LIMIT/OFFSET without ORDER BY is non-deterministic."))
		}
	}
	return out
}

func splitAtCommas(toks []*lexer.Token) [][]*lexer.Token {
	var out [][]*lexer.Token
	depth := 0
	start := 0
	for i, t := range toks {
		switch t.Kind {
		case lexer.LParen:
			depth++
		case lexer.RParen:
			if depth > 0 {
				depth--
			}
		case lexer.Comma:
			if depth == 0 {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, toks[start:])
	return out
}

func init() {
	Register(&am01{meta: newMeta("AM01", "ambiguous.distinct", "ambiguous", Suspicious, Tree, diagnostic.NoFix, "Ambiguous use of DISTINCT with GROUP BY.")})
	Register(&am02{meta: newMeta("AM02", "ambiguous.union", "ambiguous", Suspicious, Token, diagnostic.Safe, "UNION [DISTINCT|ALL] preferred over just UNION.")})
	Register(&am03{meta: newMeta("AM03", "ambiguous.order_by", "ambiguous", Suspicious, Tree, diagnostic.Unsafe, "Ambiguous ordering directions in ORDER BY.")})
	Register(&am04{meta: newMeta("AM04", "ambiguous.column_count", "ambiguous", Suspicious, Tree, diagnostic.NoFix, "Query produces unknown number of result columns.")})
	Register(&am08{meta: newMeta("AM08", "ambiguous.join_condition", "ambiguous", Suspicious, Tree, diagnostic.NoFix, "Implicit cross join detected.")})
	Register(&am09{meta: newMeta("AM09", "ambiguous.order_by_limit", "ambiguous", Suspicious, Tree, diagnostic.NoFix, "LIMIT/OFFSET without ORDER BY.")})
}
