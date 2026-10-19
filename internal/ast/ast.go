// Package ast projects a typed, trivia-free view over the lossless CST. It is
// derived on demand and owns no grammar of its own.
package ast

import (
	"strings"

	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/tree"
)

// File is the typed view of a parsed file.
type File struct {
	Tree       *tree.Tree
	Statements []*Statement
}

// Statement is one top-level statement.
type Statement struct {
	Node    *tree.Node
	Kind    tree.Kind
	Clauses []*Clause
	Select  *Select
}

// Clause is a typed clause (SELECT, FROM, WHERE, …).
type Clause struct {
	Node   *tree.Node
	Kind   tree.Kind
	Tokens []*lexer.Token
}

// Keyword returns the first significant token, upper-cased.
func (c *Clause) Keyword() string {
	if len(c.Tokens) == 0 {
		return ""
	}
	return c.Tokens[0].Upper()
}

// Text reassembles the clause from its significant tokens, space separated.
func (c *Clause) Text() string { return joinTokens(c.Tokens) }

// Select is a typed SELECT (including WITH and set operations).
type Select struct {
	Node     *tree.Node
	With     *With
	Clauses  []*Clause
	Columns  []*Column
	Tables   []*TableRef
	Joins    []*Join
	HasStar  bool
	SetOps   []*SetOp
	Selected []string
}

// With holds the CTEs of a WITH clause.
type With struct {
	Node *tree.Node
	Ctes []*Cte
}

// Cte is one common table expression.
type Cte struct {
	Node    *tree.Node
	Name    string
	Columns []string
	Body    []*lexer.Token
}

// Column is one select target.
type Column struct {
	Tokens    []*lexer.Token
	Expr      string
	Alias     string
	HasAS     bool
	Star      bool
	Qualified string
}

// TableRef is a table reference in FROM or JOIN.
type TableRef struct {
	Tokens   []*lexer.Token
	Name     string
	Parts    []string
	Alias    string
	HasAS    bool
	Subquery bool
}

// Join is a JOIN clause.
type Join struct {
	Node  *tree.Node
	Type  string
	Table *TableRef
	On    []*lexer.Token
	Using []string
}

// SetOp is a UNION/INTERSECT/EXCEPT clause.
type SetOp struct {
	Node *tree.Node
	Kind string
}

// Project builds the typed view of a file.
func Project(t *tree.Tree) *File {
	f := &File{Tree: t}
	if t == nil || t.Root == nil {
		return f
	}
	for _, n := range t.Root.Children {
		st := &Statement{Node: n, Kind: n.Kind}
		for _, c := range n.Children {
			cl := &Clause{Node: c, Kind: c.Kind, Tokens: significant(c.Leaves(nil))}
			st.Clauses = append(st.Clauses, cl)
		}
		if n.Kind == tree.Select || n.Kind == tree.Insert || hasSelectClause(st.Clauses) {
			st.Select = buildSelect(n, st.Clauses)
		}
		f.Statements = append(f.Statements, st)
	}
	return f
}

func hasSelectClause(cs []*Clause) bool {
	for _, c := range cs {
		if c.Kind == tree.SelectClause {
			return true
		}
	}
	return false
}

func buildSelect(n *tree.Node, clauses []*Clause) *Select {
	s := &Select{Node: n}
	for _, c := range clauses {
		s.Clauses = append(s.Clauses, c)
		switch c.Kind {
		case tree.With:
			s.With = buildWith(c)
		case tree.SelectClause:
			s.Columns = append(s.Columns, parseColumns(c.Tokens)...)
		case tree.FromClause:
			s.Tables = append(s.Tables, parseTables(c.Tokens)...)
		case tree.JoinClause:
			if j := parseJoin(c); j != nil {
				s.Joins = append(s.Joins, j)
			}
		case tree.SetOperation:
			if len(c.Tokens) > 0 {
				s.SetOps = append(s.SetOps, &SetOp{Node: c.Node, Kind: c.Tokens[0].Upper()})
			}
		}
	}
	for _, col := range s.Columns {
		if col.Star {
			s.HasStar = true
		}
		expr := col.Expr
		if col.Alias != "" {
			expr = col.Alias
		}
		s.Selected = append(s.Selected, expr)
	}
	return s
}

func buildWith(c *Clause) *With {
	w := &With{Node: c.Node}
	toks := c.Tokens
	i := 0
	if i < len(toks) && toks[i].Upper() == "WITH" {
		i++
	}
	if i < len(toks) && toks[i].Upper() == "RECURSIVE" {
		i++
	}
	for i < len(toks) {
		cte := &Cte{}
		// Leading trivia was stripped, so the name is the next identifier.
		for i < len(toks) && !isName(toks[i]) {
			if toks[i].Upper() == "WITH" || toks[i].Upper() == "RECURSIVE" {
				i++
				continue
			}
			i++
		}
		if i >= len(toks) || !isName(toks[i]) {
			break
		}
		cte.Name = unquote(toks[i].Text)
		i++
		if i < len(toks) && toks[i].Kind == lexer.LParen {
			var cols []string
			i++
			for i < len(toks) && toks[i].Kind != lexer.RParen {
				if isName(toks[i]) {
					cols = append(cols, unquote(toks[i].Text))
				}
				i++
			}
			if i < len(toks) {
				i++
			}
			cte.Columns = cols
		}
		for i < len(toks) && toks[i].Upper() != "AS" {
			i++
		}
		if i < len(toks) {
			i++ // AS
		}
		if i+1 < len(toks) && toks[i].Upper() == "NOT" && toks[i+1].Upper() == "MATERIALIZED" {
			i += 2
		} else if i < len(toks) && toks[i].Upper() == "MATERIALIZED" {
			i++
		}
		if i >= len(toks) || toks[i].Kind != lexer.LParen {
			break
		}
		body, next := matchParen(toks, i)
		cte.Body = body
		i = next
		w.Ctes = append(w.Ctes, cte)
		if i < len(toks) && toks[i].Kind == lexer.Comma {
			i++
			continue
		}
		break
	}
	return w
}

// matchParen returns the tokens inside the parentheses starting at open and the
// index just past the closing parenthesis.
func matchParen(toks []*lexer.Token, open int) ([]*lexer.Token, int) {
	depth := 0
	for i := open; i < len(toks); i++ {
		switch toks[i].Kind {
		case lexer.LParen:
			depth++
		case lexer.RParen:
			depth--
			if depth == 0 {
				return toks[open+1 : i], i + 1
			}
		}
	}
	return toks[open+1:], len(toks)
}

func parseColumns(toks []*lexer.Token) []*Column {
	// Skip SELECT and its modifiers.
	i := 0
	if i < len(toks) && toks[i].Upper() == "SELECT" {
		i++
	}
	for i < len(toks) {
		switch toks[i].Upper() {
		case "DISTINCT", "ALL", "DISTINCTROW", "SQL_CALC_FOUND_ROWS":
			i++
			continue
		case "TOP":
			i++
			// TOP takes a count and optional PERCENT / WITH TIES.
			if i < len(toks) && toks[i].Kind == lexer.Number {
				i++
			}
			for i < len(toks) && (toks[i].Upper() == "PERCENT" || toks[i].Upper() == "WITH" || toks[i].Upper() == "TIES") {
				i++
			}
			continue
		}
		break
	}
	parts := splitTop(toks[i:], lexer.Comma)
	var out []*Column
	for _, part := range parts {
		part = trimSemicolons(part)
		if len(part) == 0 {
			continue
		}
		col := &Column{Tokens: part}
		body, alias, hasAS := splitAlias(part)
		col.Alias, col.HasAS = alias, hasAS
		col.Expr = joinTokens(body)
		col.Star = isStar(body)
		if len(body) >= 3 && body[len(body)-1].Kind == lexer.Operator && body[len(body)-1].Text == "*" {
			col.Star = true
			col.Qualified = joinTokens(body[:len(body)-2])
		}
		out = append(out, col)
	}
	return out
}

func parseTables(toks []*lexer.Token) []*TableRef {
	i := 0
	if i < len(toks) && toks[i].Upper() == "FROM" {
		i++
	}
	var out []*TableRef
	for _, part := range splitTop(toks[i:], lexer.Comma) {
		if tr := parseTableRef(part); tr != nil {
			out = append(out, tr)
		}
	}
	return out
}

func parseTableRef(toks []*lexer.Token) *TableRef {
	toks = trimSemicolons(toks)
	if len(toks) == 0 {
		return nil
	}
	tr := &TableRef{Tokens: toks}
	body, alias, hasAS := splitAlias(toks)
	tr.Alias, tr.HasAS = alias, hasAS
	if len(body) > 0 && body[0].Kind == lexer.LParen {
		tr.Subquery = true
	} else {
		depth := 0
		for _, t := range body {
			switch t.Kind {
			case lexer.LParen:
				depth++
			case lexer.RParen:
				if depth > 0 {
					depth--
				}
			default:
				if depth == 0 && isName(t) {
					tr.Parts = append(tr.Parts, unquote(t.Text))
				}
			}
		}
	}
	tr.Name = strings.Join(tr.Parts, ".")
	return tr
}

func parseJoin(c *Clause) *Join {
	toks := c.Tokens
	if len(toks) == 0 {
		return nil
	}
	j := &Join{Node: c.Node}
	i := 0
	// Consume join modifiers up to and including JOIN.
	for i < len(toks) {
		up := toks[i].Upper()
		switch up {
		case "LEFT", "RIGHT", "FULL", "INNER", "CROSS", "NATURAL", "OUTER", "SEMI", "ANTI":
			if j.Type != "" {
				j.Type += " "
			}
			j.Type += up
			i++
			continue
		case "JOIN":
			j.Type += " JOIN"
			i++
			goto tableStart
		}
		j.Type += up
		i++
	}
tableStart:
	if j.Type == "" {
		j.Type = "JOIN"
	}
	j.Type = strings.TrimSpace(j.Type)
	// Find ON / USING at depth zero.
	end := len(toks)
	depth := 0
	for k := i; k < len(toks); k++ {
		switch toks[k].Kind {
		case lexer.LParen:
			depth++
		case lexer.RParen:
			if depth > 0 {
				depth--
			}
		case lexer.Keyword:
			if depth == 0 && (toks[k].Upper() == "ON" || toks[k].Upper() == "USING") {
				end = k
				if toks[k].Upper() == "ON" {
					j.On = toks[k+1:]
				} else {
					j.Using = usingColumns(toks[k+1:])
				}
				goto done
			}
		}
	}
done:
	j.Table = parseTableRef(toks[i:end])
	return j
}

func usingColumns(toks []*lexer.Token) []string {
	var out []string
	for _, t := range toks {
		if isName(t) {
			out = append(out, unquote(t.Text))
		}
	}
	return out
}

// trimSemicolons drops trailing statement terminators so a clause that carries
// the file's final ";" still yields a clean alias target.
func trimSemicolons(toks []*lexer.Token) []*lexer.Token {
	for len(toks) > 0 && toks[len(toks)-1].Kind == lexer.Semicolon {
		toks = toks[:len(toks)-1]
	}
	return toks
}

// splitAlias separates a target's body from its alias.
func splitAlias(toks []*lexer.Token) (body []*lexer.Token, alias string, hasAS bool) {
	toks = trimSemicolons(toks)
	// Explicit AS at depth zero wins.
	depth := 0
	for i := len(toks) - 1; i >= 0; i-- {
		switch toks[i].Kind {
		case lexer.RParen:
			depth++
		case lexer.LParen:
			depth--
		case lexer.Keyword:
			if depth == 0 && toks[i].Upper() == "AS" && i+1 < len(toks) && isName(toks[i+1]) {
				return toks[:i], unquote(toks[i+1].Text), true
			}
		}
	}
	// Implicit alias: a trailing name that is not part of a qualified path or
	// an operator expression.
	if len(toks) >= 2 && isName(toks[len(toks)-1]) {
		prev := toks[len(toks)-2]
		if prev.Kind == lexer.Dot || prev.Kind == lexer.Operator || prev.Kind == lexer.Comma {
			return toks, "", false
		}
		if prev.Kind == lexer.Keyword {
			switch prev.Upper() {
			case "END", "NULL", "TRUE", "FALSE", "ELSE":
				// CASE … END alias is a valid implicit alias.
			default:
				return toks, "", false
			}
		}
		// A single keyword cannot be an alias target.
		if len(toks) == 1 {
			return toks, "", false
		}
		return toks[:len(toks)-1], unquote(toks[len(toks)-1].Text), false
	}
	return toks, "", false
}

func isStar(toks []*lexer.Token) bool {
	if len(toks) == 1 && toks[0].Kind == lexer.Operator && toks[0].Text == "*" {
		return true
	}
	return false
}

func isName(t *lexer.Token) bool {
	return t != nil && (t.Kind == lexer.Identifier || t.Kind == lexer.QuotedIdentifier)
}

func unquote(s string) string {
	if len(s) >= 2 {
		switch s[0] {
		case '"', '`':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		case '[':
			if s[len(s)-1] == ']' {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}

func significant(toks []*lexer.Token) []*lexer.Token {
	out := make([]*lexer.Token, 0, len(toks))
	for _, t := range toks {
		if t.IsSignificant() {
			out = append(out, t)
		}
	}
	return out
}

func splitTop(toks []*lexer.Token, sep lexer.Kind) [][]*lexer.Token {
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
		case sep:
			if depth == 0 {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, toks[start:])
	return out
}

func joinTokens(toks []*lexer.Token) string {
	var b strings.Builder
	for i, t := range toks {
		if i > 0 && needsSpace(toks[i-1], t) {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

func needsSpace(prev, cur *lexer.Token) bool {
	switch cur.Kind {
	case lexer.Comma, lexer.Dot, lexer.RParen, lexer.Semicolon:
		return false
	}
	switch prev.Kind {
	case lexer.LParen, lexer.Dot:
		return false
	}
	return true
}
