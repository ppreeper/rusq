package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/source"
)

func sourceRange(start, end int) source.Range {
	return source.Range{Start: start, End: end}
}

// nextSig returns the index of the next significant token after i, or -1.
func nextSig(tokens []lexer.Token, i int) int {
	for j := i + 1; j < len(tokens); j++ {
		if tokens[j].IsSignificant() {
			return j
		}
	}
	return -1
}

// prevSig returns the index of the previous significant token before i, or -1.
func prevSig(tokens []lexer.Token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if tokens[j].IsSignificant() {
			return j
		}
	}
	return -1
}

// nextSigPtr is nextSig for a slice of token pointers.
func nextSigPtr(tokens []*lexer.Token, i int) int {
	for j := i + 1; j < len(tokens); j++ {
		if tokens[j].IsSignificant() {
			return j
		}
	}
	return -1
}

// prevSigPtr is prevSig for a slice of token pointers.
func prevSigPtr(tokens []*lexer.Token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if tokens[j].IsSignificant() {
			return j
		}
	}
	return -1
}

func isNameToken(k lexer.Kind) bool {
	return k == lexer.Identifier || k == lexer.QuotedIdentifier
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

func joinText(tokens []lexer.Token, from, to int) string {
	var b strings.Builder
	for i := from; i < to && i < len(tokens); i++ {
		b.WriteString(tokens[i].Text)
	}
	return b.String()
}

var datatypes = map[string]struct{}{
	"INT": {}, "INTEGER": {}, "SMALLINT": {}, "BIGINT": {}, "TINYINT": {},
	"DECIMAL": {}, "NUMERIC": {}, "FLOAT": {}, "REAL": {}, "DOUBLE": {},
	"VARCHAR": {}, "CHAR": {}, "TEXT": {}, "NVARCHAR": {}, "NCHAR": {},
	"DATE": {}, "TIME": {}, "TIMESTAMP": {}, "DATETIME": {}, "DATETIME2": {},
	"DATETIMEOFFSET": {}, "BOOLEAN": {}, "BOOL": {}, "BLOB": {}, "JSON": {},
	"JSONB": {}, "UUID": {}, "BINARY": {}, "VARBINARY": {}, "INTERVAL": {},
	"SERIAL": {}, "BIGSERIAL": {}, "MONEY": {}, "BYTEA": {},
}

func isDatatype(s string) bool {
	_, ok := datatypes[strings.ToUpper(s)]
	return ok
}

var nonFunctionKeywords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "GROUP": {}, "ORDER": {}, "BY": {},
	"HAVING": {}, "LIMIT": {}, "OFFSET": {}, "JOIN": {}, "LEFT": {}, "RIGHT": {},
	"FULL": {}, "INNER": {}, "OUTER": {}, "CROSS": {}, "NATURAL": {}, "ON": {},
	"USING": {}, "AND": {}, "OR": {}, "NOT": {}, "IN": {}, "EXISTS": {},
	"CASE": {}, "WHEN": {}, "THEN": {}, "ELSE": {}, "END": {}, "AS": {},
	"IS": {}, "NULL": {}, "TRUE": {}, "FALSE": {}, "LIKE": {}, "BETWEEN": {},
	"UNION": {}, "INTERSECT": {}, "EXCEPT": {}, "MINUS": {}, "VALUES": {},
	"INTO": {}, "SET": {}, "DISTINCT": {}, "ALL": {}, "OVER": {}, "PARTITION": {},
	"WITH": {}, "RETURNING": {}, "INSERT": {}, "UPDATE": {}, "DELETE": {},
}

func isFunctionKeyword(t lexer.Token) bool {
	if t.Kind != lexer.Keyword {
		return false
	}
	_, bad := nonFunctionKeywords[t.Upper()]
	return !bad
}
