// Package parser builds an error-recovering lossless CST from a token stream.
// The grammar is deliberately permissive: malformed regions become Error nodes
// and the rest of the file still parses.
package parser

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// Parser bounds pathological inputs.
type Parser struct {
	MaxDepth int
	MaxNodes int
}

// New returns a Parser with the default limits.
func New() *Parser { return &Parser{MaxDepth: 255, MaxNodes: 100000} }

// Parse parses tokens with the default limits.
func Parse(tokens []lexer.Token, d *dialect.Dialect) (*tree.Tree, []diagnostic.SyntaxError) {
	return New().Parse(tokens, d)
}

// Unparse reassembles the source from the tree.
func Unparse(t *tree.Tree) []byte { return tree.Unparse(t) }

// Parse builds a CST and returns any recoverable syntax errors.
func (p *Parser) Parse(tokens []lexer.Token, d *dialect.Dialect) (*tree.Tree, []diagnostic.SyntaxError) {
	if d == nil {
		d = dialect.MustLookup("ansi")
	}
	root := tree.New(tree.Root)
	var errs []diagnostic.SyntaxError

	if p.MaxNodes > 0 && len(tokens) > p.MaxNodes {
		errs = append(errs, diagnostic.SyntaxError{
			Span:    source.Range{Start: 0, End: len(tokens)},
			Message: "token count exceeds max-parse-nodes",
		})
	}

	segments := splitStatements(tokens)
	for _, seg := range segments {
		root.Append(p.parseStatement(seg, &errs))
	}

	// Preserve any tokens not consumed by a statement (EOF is dropped from the
	// tree; it carries no bytes).
	for _, tok := range tokens {
		if tok.Kind == lexer.EOF {
			continue
		}
	}

	t := &tree.Tree{Root: root, Dialect: d.Name, Errors: errs}
	t.Source = tree.Unparse(t)
	return t, errs
}

func (p *Parser) parseStatement(tokens []lexer.Token, errs *[]diagnostic.SyntaxError) *tree.Node {
	kind, first := classifyStatement(tokens)
	if first < 0 {
		// Trivia-only segment.
		return wrapLeaves(tree.Unknown, tokens)
	}

	stmt := tree.New(kind)
	depth := 0
	clauseStart := 0
	clauseKind := preambleKind(kind)
	clauses := 0
	joinSeen := false

	flush := func(end int) {
		if end <= clauseStart {
			return
		}
		stmt.Append(wrapLeaves(clauseKind, tokens[clauseStart:end]))
		clauses++
	}

	for i, tok := range tokens {
		switch tok.Kind {
		case lexer.LParen:
			depth++
			if p.MaxDepth > 0 && depth > p.MaxDepth {
				*errs = append(*errs, diagnostic.SyntaxError{
					Span:    tok.Span,
					Message: "nesting exceeds max-parse-depth",
				})
				depth = p.MaxDepth
			}
		case lexer.RParen:
			if depth > 0 {
				depth--
			}
		case lexer.Error:
			*errs = append(*errs, diagnostic.SyntaxError{
				Span:    tok.Span,
				Message: "unexpected character " + strings.TrimSpace(tok.Text),
			})
		}

		if depth != 0 || !tok.IsSignificant() {
			continue
		}
		if tok.Kind != lexer.Keyword {
			continue
		}
		k, ok := clauseKindFor(statementContext(tokens, i), tok.Upper())
		if !ok {
			continue
		}
		// Do not split on the first token; it opened the statement.
		if i == first {
			clauseKind = k
			continue
		}
		// A run of join modifiers ending in JOIN is a single clause head:
		// LEFT OUTER JOIN / INNER JOIN must not be split into two clauses.
		if k == tree.JoinClause && clauseKind == tree.JoinClause && !joinSeen {
			if tok.Upper() == "JOIN" {
				joinSeen = true
			}
			continue
		}
		flush(i)
		clauseStart = i
		clauseKind = k
		joinSeen = k == tree.JoinClause && tok.Upper() == "JOIN"
	}
	// Keep the statement terminator out of the final clause so rules that read
	// clause tokens do not see a trailing ";".
	if clauses == 0 {
		stmt.Append(wrapLeaves(tree.Unknown, tokens))
	} else {
		end := len(tokens)
		for end > clauseStart && tokens[end-1].Kind == lexer.Semicolon {
			end--
		}
		flush(end)
		for i := end; i < len(tokens); i++ {
			if tokens[i].Kind == lexer.EOF {
				continue
			}
			stmt.Append(tree.Leaf(&tokens[i]))
		}
	}

	// Mark an unclosed parenthesis as a recoverable error.
	if depth > 0 {
		*errs = append(*errs, diagnostic.SyntaxError{
			Span:    tokens[len(tokens)-1].Span,
			Message: "unclosed parenthesis",
		})
		stmt.Kind = tree.Error
	} else if hasErrorToken(tokens) {
		stmt.Kind = tree.Error
	}
	return stmt
}

func wrapLeaves(kind tree.Kind, tokens []lexer.Token) *tree.Node {
	n := tree.New(kind)
	for i := range tokens {
		if tokens[i].Kind == lexer.EOF {
			continue
		}
		n.Append(tree.Leaf(&tokens[i]))
	}
	return n
}

func hasErrorToken(tokens []lexer.Token) bool {
	for _, t := range tokens {
		if t.Kind == lexer.Error {
			return true
		}
	}
	return false
}

func splitStatements(tokens []lexer.Token) [][]lexer.Token {
	var out [][]lexer.Token
	depth := 0
	start := 0
	for i, t := range tokens {
		if t.Kind == lexer.EOF {
			continue
		}
		switch t.Kind {
		case lexer.LParen:
			depth++
		case lexer.RParen:
			if depth > 0 {
				depth--
			}
		}
		if t.Kind == lexer.Semicolon && depth == 0 {
			out = append(out, tokens[start:i+1])
			start = i + 1
		}
	}
	if start < len(tokens) {
		seg := tokens[start:]
		if containsBytes(seg) {
			out = append(out, seg)
		}
	}
	return out
}

func containsBytes(tokens []lexer.Token) bool {
	for _, t := range tokens {
		if t.Kind != lexer.EOF {
			return true
		}
	}
	return false
}

// classifyStatement returns the statement kind and the index of its first
// significant token.
func classifyStatement(tokens []lexer.Token) (tree.Kind, int) {
	for i, t := range tokens {
		if !t.IsSignificant() {
			continue
		}
		if t.Kind != lexer.Keyword {
			return tree.Unknown, i
		}
		switch t.Upper() {
		case "SELECT", "WITH":
			return tree.Select, i
		case "INSERT", "REPLACE":
			return tree.Insert, i
		case "UPDATE":
			return tree.Update, i
		case "DELETE", "TRUNCATE":
			return tree.Delete, i
		case "CREATE":
			return tree.Create, i
		case "ALTER":
			return tree.Alter, i
		case "DROP":
			return tree.Drop, i
		case "MERGE":
			return tree.Merge, i
		case "BEGIN", "COMMIT", "ROLLBACK", "SET", "USE", "PRAGMA", "GRANT", "REVOKE", "EXPLAIN":
			return tree.Batch, i
		default:
			return tree.Unknown, i
		}
	}
	return tree.Unknown, -1
}

// statementContext returns a coarse context used to disambiguate clause
// keywords (e.g. VALUES is a clause in INSERT, not in a plain expression).
func statementContext(tokens []lexer.Token, upTo int) string {
	for i := 0; i < upTo; i++ {
		if tokens[i].Kind == lexer.Keyword {
			switch tokens[i].Upper() {
			case "INSERT", "REPLACE":
				return "insert"
			case "UPDATE":
				return "update"
			case "DELETE":
				return "delete"
			}
		}
	}
	return "select"
}

// clauseKindFor maps a top-level keyword to a clause kind. ok is false for
// keywords that do not begin a clause.
func clauseKindFor(ctx, kw string) (tree.Kind, bool) {
	switch kw {
	case "SELECT":
		return tree.SelectClause, true
	case "FROM":
		return tree.FromClause, true
	case "WHERE":
		return tree.WhereClause, true
	case "GROUP":
		return tree.GroupByClause, true
	case "HAVING":
		return tree.HavingClause, true
	case "ORDER":
		return tree.OrderByClause, true
	case "LIMIT", "TOP", "FETCH":
		return tree.LimitClause, true
	case "OFFSET":
		return tree.OffsetClause, true
	case "UNION", "INTERSECT", "EXCEPT", "MINUS":
		return tree.SetOperation, true
	case "JOIN", "LEFT", "RIGHT", "FULL", "INNER", "CROSS", "NATURAL":
		return tree.JoinClause, true
	case "WITH":
		return tree.With, true
	case "VALUES":
		if ctx == "insert" {
			return tree.ValuesClause, true
		}
		return tree.Unknown, false
	case "RETURNING":
		if ctx == "insert" || ctx == "update" || ctx == "delete" {
			return tree.ReturningClause, true
		}
		return tree.Unknown, false
	case "SET":
		if ctx == "update" {
			return tree.Expression, true
		}
		return tree.Unknown, false
	}
	return tree.Unknown, false
}

// preambleKind names the leading clause of a statement.
func preambleKind(stmt tree.Kind) tree.Kind {
	switch stmt {
	case tree.Select:
		return tree.With
	case tree.Insert, tree.Update, tree.Delete:
		return tree.Statement
	case tree.Create, tree.Alter, tree.Drop:
		return tree.Statement
	}
	return tree.Statement
}
