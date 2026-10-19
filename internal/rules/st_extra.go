package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/tree"
)

// ST02 — unnecessary CASE statement.
type st02 struct{ meta Meta }

func (r *st02) Meta() Meta { return r.meta }
func (r *st02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || t.Upper() != "CASE" {
			continue
		}
		depth, whens := 0, 0
		for j := i; j < len(ctx.Tokens); j++ {
			tok := ctx.Tokens[j]
			if tok.Kind != lexer.Keyword {
				continue
			}
			switch tok.Upper() {
			case "CASE":
				depth++
			case "END":
				depth--
				if depth == 0 {
					j = len(ctx.Tokens)
				}
			case "WHEN":
				if depth == 1 {
					whens++
				}
			}
		}
		if whens == 1 {
			out = append(out, diag(r.meta, t.Span, "CASE expression can be simplified."))
		}
	}
	return out
}

// ST04 — nested CASE in ELSE.
type st04 struct{ meta Meta }

func (r *st04) Meta() Meta { return r.meta }
func (r *st04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || t.Upper() != "ELSE" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n >= 0 && ctx.Tokens[n].Kind == lexer.Keyword && ctx.Tokens[n].Upper() == "CASE" {
			out = append(out, diag(r.meta, t.Span, "Nested CASE in ELSE can be flattened."))
		}
	}
	return out
}

// ST05 — prefer CTEs over subqueries in FROM/JOIN.
type st05 struct{ meta Meta }

func (r *st05) Meta() Meta { return r.meta }
func (r *st05) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, tr := range allTables(st.Select) {
			if tr.Subquery {
				out = append(out, diag(r.meta, tr.Tokens[0].Span, "Use a CTE instead of a subquery in FROM/JOIN."))
			}
		}
	}
	return out
}

// ST06 — column order within SELECT.
type st06 struct{ meta Meta }

func (r *st06) Meta() Meta { return r.meta }
func (r *st06) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		maxRank := -1
		for _, col := range st.Select.Columns {
			rank := 2
			if col.Star {
				rank = 0
			} else if simpleColumnExpr(col.Tokens) {
				rank = 1
			}
			if rank < maxRank {
				out = append(out, diag(r.meta, col.Tokens[0].Span, "Column order should be wildcards, then simple targets, then expressions."))
			}
			if rank > maxRank {
				maxRank = rank
			}
		}
	}
	return out
}

// ST07 — prefer explicit join keys over USING.
type st07 struct{ meta Meta }

func (r *st07) Meta() Meta { return r.meta }
func (r *st07) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, j := range st.Select.Joins {
			if len(j.Using) > 0 {
				out = append(out, diag(r.meta, j.Node.Span(), "Specify join keys explicitly instead of using USING."))
			}
		}
	}
	return out
}

// ST09 — join condition table order.
type st09 struct{ meta Meta }

func (r *st09) Meta() Meta { return r.meta }
func (r *st09) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	policy := ctx.Settings.RuleString(r.meta.Name, "preferred-first-table-in-join-clause", "earlier")
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, j := range st.Select.Joins {
			if j.Table == nil || len(j.On) < 7 {
				continue
			}
			joined := strings.ToLower(j.Table.Alias)
			if joined == "" {
				joined = strings.ToLower(j.Table.Name)
			}
			toks := j.On
			for i := 0; i+6 < len(toks); i++ {
				if !isNameToken(toks[i].Kind) || toks[i+1].Kind != lexer.Dot || !isNameToken(toks[i+2].Kind) ||
					toks[i+3].Kind != lexer.Operator || toks[i+3].Text != "=" ||
					!isNameToken(toks[i+4].Kind) || toks[i+5].Kind != lexer.Dot || !isNameToken(toks[i+6].Kind) {
					continue
				}
				q1 := strings.ToLower(unquote(toks[i].Text))
				q2 := strings.ToLower(unquote(toks[i+4].Text))
				bad := false
				if policy == "later" {
					bad = q2 == joined
				} else {
					bad = q1 == joined
				}
				if bad {
					out = append(out, diag(r.meta, toks[i].Span, "Join condition should list the earlier table first."))
					break
				}
			}
		}
	}
	return out
}

// ST10 — redundant constant expressions.
type st10 struct{ meta Meta }

func (r *st10) Meta() Meta { return r.meta }
func (r *st10) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.WhereClause && c.Kind != tree.HavingClause {
				continue
			}
			toks := c.Tokens
			start := 0
			if len(toks) > 0 && (toks[0].Upper() == "WHERE" || toks[0].Upper() == "HAVING") {
				start = 1
			}
			for _, item := range splitTopKeyword(toks[start:], "AND") {
				if len(item) > 0 && item[len(item)-1].Kind == lexer.Semicolon {
					item = item[:len(item)-1]
				}
				if len(item) == 1 && item[0].Kind == lexer.Keyword && item[0].Upper() == "TRUE" {
					out = append(out, diag(r.meta, item[0].Span, "Redundant constant expression."))
					continue
				}
				if len(item) == 3 && isLiteral(item[0]) && item[1].Kind == lexer.Operator && item[1].Text == "=" && isLiteral(item[2]) {
					out = append(out, diag(r.meta, item[0].Span, "Redundant constant expression."))
				}
			}
		}
	}
	return out
}

func isLiteral(t *lexer.Token) bool {
	if t == nil {
		return false
	}
	if t.Kind == lexer.Number || t.Kind == lexer.String {
		return true
	}
	u := t.Upper()
	return u == "TRUE" || u == "FALSE" || u == "NULL"
}

func splitTopKeyword(toks []*lexer.Token, kw string) [][]*lexer.Token {
	var out [][]*lexer.Token
	depth, start := 0, 0
	for i, t := range toks {
		switch t.Kind {
		case lexer.LParen:
			depth++
		case lexer.RParen:
			if depth > 0 {
				depth--
			}
		case lexer.Keyword:
			if depth == 0 && t.Upper() == kw {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, toks[start:])
	return out
}

// ST11 — joined table is never referenced.
type st11 struct{ meta Meta }

func (r *st11) Meta() Meta { return r.meta }
func (r *st11) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || st.Select.HasStar || len(st.Select.Joins) == 0 {
			continue
		}
		sigs := significantOf(st.Node.Leaves(nil))
		for _, j := range st.Select.Joins {
			if j.Table == nil {
				continue
			}
			names := []string{}
			if j.Table.Alias != "" {
				names = append(names, j.Table.Alias)
			}
			if j.Table.Name != "" {
				names = append(names, j.Table.Name)
			}
			used := false
			for _, n := range names {
				count := 0
				for _, t := range sigs {
					if isNameToken(t.Kind) && strings.EqualFold(unquote(t.Text), n) {
						count++
					}
				}
				if count > 1 {
					used = true
					break
				}
			}
			if !used {
				out = append(out, diag(r.meta, j.Node.Span(), "Joined table is not referenced in the query."))
			}
		}
	}
	return out
}

func init() {
	Register(&st02{meta: newMeta("ST02", "structure.simple_case", "structure", Style, Token, diagnostic.NoFix, "Unnecessary CASE statement.")})
	Register(&st04{meta: newMeta("ST04", "structure.nested_case", "structure", Style, Token, diagnostic.NoFix, "Nested CASE in ELSE could be flattened.")})
	Register(&st05{meta: newMeta("ST05", "structure.subquery", "structure", Style, Tree, diagnostic.NoFix, "Join/From clauses should not contain subqueries; use CTEs.", "forbid-subquery-in")})
	Register(&st06{meta: newMeta("ST06", "structure.column_order", "structure", Style, Tree, diagnostic.NoFix, "Select wildcards then simple targets before calculations and aggregates.")})
	Register(&st07{meta: newMeta("ST07", "structure.using", "structure", Style, Tree, diagnostic.NoFix, "Prefer specifying join keys instead of USING.")})
	Register(&st09{meta: newMeta("ST09", "structure.join_condition_order", "structure", Style, Tree, diagnostic.NoFix, "Joins should list the table referenced earlier first.", "preferred-first-table-in-join-clause")})
	Register(&st10{meta: newMeta("ST10", "structure.constant_expression", "structure", Suspicious, Tree, diagnostic.NoFix, "Redundant constant expression.")})
	Register(&st11{meta: newMeta("ST11", "structure.unused_join", "structure", Suspicious, Tree, diagnostic.NoFix, "Joined table is not referenced in the query.")})
}
