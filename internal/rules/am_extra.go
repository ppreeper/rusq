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

// AM05 — joins should be fully qualified.
type am05 struct{ meta Meta }

func (r *am05) Meta() Meta { return r.meta }
func (r *am05) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	policy := ctx.Settings.RuleString(r.meta.Name, "fully-qualify-join-types", "inner")
	qualifyInner := policy == "inner" || policy == "both"
	qualifyOuter := policy == "outer" || policy == "both"
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, j := range st.Select.Joins {
			typ := strings.ToUpper(j.Type)
			bare := typ == "JOIN"
			inner := strings.Contains(typ, "INNER")
			outer := strings.Contains(typ, "LEFT") || strings.Contains(typ, "RIGHT") || strings.Contains(typ, "FULL")
			if (bare && (qualifyInner || qualifyOuter)) || (inner && qualifyInner && typ == "INNER") {
				d := diag(r.meta, j.Node.Span(), "Join clauses should be fully qualified.")
				if bare {
					d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(j.Node.Span().Start), "INNER ")}}
					d.Fixability = r.meta.Fixable
				}
				out = append(out, d)
			} else if outer && qualifyOuter && !strings.Contains(typ, "OUTER") {
				out = append(out, diag(r.meta, j.Node.Span(), "Outer joins should specify OUTER."))
			}
		}
	}
	return out
}

// AM06 — consistent column references in GROUP BY / ORDER BY.
type am06 struct{ meta Meta }

func (r *am06) Meta() Meta { return r.meta }
func (r *am06) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	policy := ctx.Settings.RuleString(r.meta.Name, "group-by-and-order-by-style", "consistent")
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.GroupByClause && c.Kind != tree.OrderByClause {
				continue
			}
			items := splitAtCommas(stripByPrefix(c.Tokens))
			if len(items) < 2 {
				continue
			}
			var positional, named int
			for _, item := range items {
				if len(item) == 1 && item[0].Kind == lexer.Number {
					positional++
				} else if len(item) > 0 {
					named++
				}
			}
			wantPos := policy == "implicit"
			if policy == "consistent" {
				if positional == 0 || named == 0 {
					continue
				}
				wantPos = positional > named
			}
			for _, item := range items {
				isPos := len(item) == 1 && item[0].Kind == lexer.Number
				if isPos != wantPos && len(item) > 0 {
					target := "column names"
					if wantPos {
						target = "positions"
					}
					out = append(out, diag(r.meta, item[0].Span, "Inconsistent column references; use "+target+"."))
				}
			}
		}
	}
	return out
}

// AM07 — set query branches must have the same column count.
type am07 struct{ meta Meta }

func (r *am07) Meta() Meta { return r.meta }
func (r *am07) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || len(st.Select.SetOps) == 0 {
			continue
		}
		var counts []int
		var setNode *tree.Node
		for _, c := range st.Clauses {
			switch c.Kind {
			case tree.SelectClause:
				counts = append(counts, countSelectTargets(c))
			case tree.SetOperation:
				if setNode == nil {
					setNode = c.Node
				}
			}
		}
		if len(counts) < 2 {
			continue
		}
		first := counts[0]
		for _, n := range counts[1:] {
			if n != first {
				span := st.Select.Node.Span()
				if setNode != nil {
					span = setNode.Span()
				}
				out = append(out, diag(r.meta, span, "Queries within a set operation produce different numbers of columns."))
				break
			}
		}
	}
	return out
}

func stripByPrefix(toks []*lexer.Token) []*lexer.Token {
	if len(toks) >= 2 {
		u := toks[0].Upper()
		if (u == "GROUP" || u == "ORDER") && toks[1].Upper() == "BY" {
			return toks[2:]
		}
	}
	return toks
}

func countSelectTargets(c *ast.Clause) int {
	if c == nil {
		return 0
	}
	i := 0
	if i < len(c.Tokens) && c.Tokens[i].Upper() == "SELECT" {
		i++
	}
	for i < len(c.Tokens) {
		switch c.Tokens[i].Upper() {
		case "DISTINCT", "ALL", "DISTINCTROW":
			i++
			continue
		}
		break
	}
	n := 0
	for _, part := range splitAtCommas(c.Tokens[i:]) {
		if len(part) > 0 {
			n++
		}
	}
	return n
}

func init() {
	Register(&am05{meta: newMeta("AM05", "ambiguous.join", "ambiguous", Style, Tree, diagnostic.Safe, "Join clauses should be fully qualified.", "fully-qualify-join-types")})
	Register(&am06{meta: newMeta("AM06", "ambiguous.column_references", "ambiguous", Style, Tree, diagnostic.NoFix, "Inconsistent column references in GROUP BY/ORDER BY.", "group-by-and-order-by-style")})
	Register(&am07{meta: newMeta("AM07", "ambiguous.set_columns", "ambiguous", Correctness, Tree, diagnostic.NoFix, "Queries within a set operation produce different numbers of columns.")})
}
