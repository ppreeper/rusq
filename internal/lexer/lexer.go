// Package lexer turns SQL source into a lossless token stream. Whitespace,
// newlines, and comments are preserved as trivia so the stream can be
// reassembled byte-for-byte.
package lexer

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/source"
)

// Kind classifies a token.
type Kind uint8

const (
	EOF Kind = iota
	Whitespace
	Newline
	Comment
	Keyword
	Identifier
	QuotedIdentifier
	String
	Number
	Operator
	Comma
	Dot
	Semicolon
	LParen
	RParen
	LBracket
	RBracket
	Placeholder
	Error
)

func (k Kind) String() string {
	switch k {
	case EOF:
		return "eof"
	case Whitespace:
		return "whitespace"
	case Newline:
		return "newline"
	case Comment:
		return "comment"
	case Keyword:
		return "keyword"
	case Identifier:
		return "identifier"
	case QuotedIdentifier:
		return "quoted_identifier"
	case String:
		return "string"
	case Number:
		return "number"
	case Operator:
		return "operator"
	case Comma:
		return "comma"
	case Dot:
		return "dot"
	case Semicolon:
		return "semicolon"
	case LParen:
		return "lparen"
	case RParen:
		return "rparen"
	case LBracket:
		return "lbracket"
	case RBracket:
		return "rbracket"
	case Placeholder:
		return "placeholder"
	case Error:
		return "error"
	}
	return "unknown"
}

// Token is a single lexical unit.
type Token struct {
	Kind Kind
	Text string
	Span source.Range
	Line int
	Col  int
}

// IsTrivia reports whether the token is whitespace, a newline, or a comment.
func (t Token) IsTrivia() bool {
	return t.Kind == Whitespace || t.Kind == Newline || t.Kind == Comment
}

// IsSignificant reports whether the token carries syntax.
func (t Token) IsSignificant() bool { return !t.IsTrivia() && t.Kind != EOF }

// Upper returns the token text upper-cased. It is only meaningful for
// keywords and identifiers.
func (t Token) Upper() string { return strings.ToUpper(t.Text) }

// Lex tokenises src. Lexing never fails: malformed input becomes Error tokens
// and malformed strings/comments are reported as syntax errors.
func Lex(src source.Text, d *dialect.Dialect) ([]Token, []diagnostic.SyntaxError) {
	if d == nil {
		d = dialect.MustLookup("ansi")
	}
	l := &lexer{src: src.Bytes, d: d, line: 1, col: 1}
	l.run()
	return l.tokens, l.errs
}

type lexer struct {
	src    []byte
	d      *dialect.Dialect
	i      int
	line   int
	col    int
	tokens []Token
	errs   []diagnostic.SyntaxError
}

func (l *lexer) run() {
	for l.i < len(l.src) {
		c := l.src[l.i]
		switch {
		case c == ' ' || c == '\t' || c == '\f' || c == '\v':
			l.whitespace()
		case c == '\n' || c == '\r':
			l.newline()
		case c == '-' && l.peek(1) == '-':
			l.lineComment()
		case c == '/' && l.peek(1) == '*':
			l.blockComment()
		case c == '\'':
			l.quoted('\'', String)
		case l.isStrOpen(c):
			l.quoted(c, String)
		case l.isIdentOpen(c):
			l.quotedIdentifier(c)
		case c == '$':
			if l.d.DollarQuoting && l.dollarQuote() {
				continue
			}
			if isDigit(l.peek(1)) {
				l.placeholder()
			} else if isIdentStart(l.peek(1)) {
				l.placeholder()
			} else {
				l.operator()
			}
		case isDigit(c):
			l.number()
		case c == '.' && isDigit(l.peek(1)):
			l.number()
		case isIdentStart(c):
			l.word()
		case c == ':':
			if l.peek(1) == ':' {
				l.operator()
			} else if isIdentStart(l.peek(1)) {
				l.placeholder()
			} else {
				l.operator()
			}
		case c == '?' || c == '@' || c == '&':
			if isIdentStart(l.peek(1)) || c == '?' {
				l.placeholder()
			} else {
				l.operator()
			}
		case c == '%' && (l.peek(1) == 's' || l.peek(1) == '('):
			l.placeholder()
		case c == ',':
			l.single(Comma)
		case c == '.':
			l.single(Dot)
		case c == ';':
			l.single(Semicolon)
		case c == '(':
			l.single(LParen)
		case c == ')':
			l.single(RParen)
		case c == '[' && !l.isIdentOpen('['):
			l.single(LBracket)
		case c == ']' && !l.isIdentClose(']'):
			l.single(RBracket)
		case c == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X'):
			l.number()
		default:
			l.operator()
		}
	}
	// EOF is zero-width at the end of the file.
	l.emit(EOF, l.i, l.i)
}

func (l *lexer) peek(n int) byte {
	if l.i+n >= len(l.src) {
		return 0
	}
	return l.src[l.i+n]
}

func (l *lexer) emit(k Kind, start, end int) {
	t := Token{
		Kind: k,
		Text: string(l.src[start:end]),
		Span: source.NewRange(start, end),
		Line: l.line,
		Col:  l.col,
	}
	l.tokens = append(l.tokens, t)
}

// emitAdvance emits a token spanning [start,l.i) and advances the line/column
// tracker over the token text.
func (l *lexer) emitAdvance(k Kind, start int) {
	end := l.i
	l.emit(k, start, end)
	l.advance(l.src[start:end])
}

func (l *lexer) advance(b []byte) {
	for _, c := range b {
		if c == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
	}
}

func (l *lexer) single(k Kind) {
	start := l.i
	l.i++
	l.emitAdvance(k, start)
}

func (l *lexer) whitespace() {
	start := l.i
	for l.i < len(l.src) && (l.src[l.i] == ' ' || l.src[l.i] == '\t' || l.src[l.i] == '\f' || l.src[l.i] == '\v') {
		l.i++
	}
	l.emitAdvance(Whitespace, start)
}

func (l *lexer) newline() {
	start := l.i
	if l.src[l.i] == '\r' && l.peek(1) == '\n' {
		l.i += 2
	} else {
		l.i++
	}
	l.emitAdvance(Newline, start)
}

func (l *lexer) lineComment() {
	start := l.i
	for l.i < len(l.src) && l.src[l.i] != '\n' && l.src[l.i] != '\r' {
		l.i++
	}
	l.emitAdvance(Comment, start)
}

func (l *lexer) blockComment() {
	start := l.i
	l.i += 2
	depth := 1
	for l.i < len(l.src) && depth > 0 {
		if l.src[l.i] == '/' && l.peek(1) == '*' {
			depth++
			l.i += 2
			continue
		}
		if l.src[l.i] == '*' && l.peek(1) == '/' {
			depth--
			l.i += 2
			continue
		}
		l.i++
	}
	if depth > 0 {
		l.errs = append(l.errs, diagnostic.SyntaxError{
			Span:    source.NewRange(start, l.i),
			Message: "unterminated block comment",
		})
	}
	l.emitAdvance(Comment, start)
}

func (l *lexer) quoted(quote byte, kind Kind) {
	start := l.i
	l.i++
	closed := false
	for l.i < len(l.src) {
		c := l.src[l.i]
		if c == '\\' && l.i+1 < len(l.src) {
			l.i += 2
			continue
		}
		if c == quote {
			if l.peek(1) == quote {
				l.i += 2
				continue
			}
			l.i++
			closed = true
			break
		}
		l.i++
	}
	if !closed {
		l.errs = append(l.errs, diagnostic.SyntaxError{
			Span:    source.NewRange(start, l.i),
			Message: "unterminated string literal",
		})
	}
	l.emitAdvance(kind, start)
}

func (l *lexer) quotedIdentifier(open byte) {
	closeCh := l.identClose(open)
	start := l.i
	l.i++
	closed := false
	for l.i < len(l.src) {
		if l.src[l.i] == closeCh {
			if l.peek(1) == closeCh {
				l.i += 2
				continue
			}
			l.i++
			closed = true
			break
		}
		l.i++
	}
	if !closed {
		l.errs = append(l.errs, diagnostic.SyntaxError{
			Span:    source.NewRange(start, l.i),
			Message: "unterminated quoted identifier",
		})
	}
	l.emitAdvance(QuotedIdentifier, start)
}

func (l *lexer) dollarQuote() bool {
	// A dollar quote is $tag$ ... $tag$ with an optional identifier tag.
	j := l.i + 1
	for j < len(l.src) && (isIdentStart(l.src[j]) || isDigit(l.src[j])) {
		j++
	}
	if j >= len(l.src) || l.src[j] != '$' {
		return false
	}
	delim := l.src[l.i : j+1]
	start := l.i
	l.i = j + 1
	idx := indexBytes(l.src[l.i:], delim)
	if idx < 0 {
		l.i = len(l.src)
		l.errs = append(l.errs, diagnostic.SyntaxError{
			Span:    source.NewRange(start, l.i),
			Message: "unterminated dollar-quoted string",
		})
		l.emitAdvance(String, start)
		return true
	}
	l.i += idx + len(delim)
	l.emitAdvance(String, start)
	return true
}

func (l *lexer) placeholder() {
	start := l.i
	switch l.src[l.i] {
	case ':':
		l.i++
		for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
			l.i++
		}
	case '?':
		l.i++
	case '$':
		l.i++
		for l.i < len(l.src) && isDigit(l.src[l.i]) {
			l.i++
		}
	case '@', '&':
		l.i++
		for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
			l.i++
		}
	case '%':
		l.i++
		if l.i < len(l.src) && l.src[l.i] == '(' {
			for l.i < len(l.src) && l.src[l.i] != ')' {
				l.i++
			}
			if l.i < len(l.src) {
				l.i++
			}
		} else if l.i < len(l.src) && l.src[l.i] == 's' {
			l.i++
		}
	}
	l.emitAdvance(Placeholder, start)
}

func (l *lexer) number() {
	start := l.i
	if l.src[l.i] == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X') {
		l.i += 2
		for l.i < len(l.src) && isHex(l.src[l.i]) {
			l.i++
		}
		l.emitAdvance(Number, start)
		return
	}
	for l.i < len(l.src) && isDigit(l.src[l.i]) {
		l.i++
	}
	if l.i < len(l.src) && l.src[l.i] == '.' {
		l.i++
		for l.i < len(l.src) && isDigit(l.src[l.i]) {
			l.i++
		}
	}
	if l.i < len(l.src) && (l.src[l.i] == 'e' || l.src[l.i] == 'E') {
		j := l.i + 1
		if j < len(l.src) && (l.src[j] == '+' || l.src[j] == '-') {
			j++
		}
		if j < len(l.src) && isDigit(l.src[j]) {
			l.i = j
			for l.i < len(l.src) && isDigit(l.src[l.i]) {
				l.i++
			}
		}
	}
	l.emitAdvance(Number, start)
}

func (l *lexer) word() {
	start := l.i
	for l.i < len(l.src) && isIdentPart(l.src[l.i]) {
		l.i++
	}
	kind := Identifier
	if l.isKeyword(l.src[start:l.i]) {
		kind = Keyword
	}
	l.emitAdvance(kind, start)
}

func (l *lexer) operator() {
	start := l.i
	// Longest-match first.
	for _, op := range operators {
		if l.hasPrefix(op) {
			l.i += len(op)
			l.emitAdvance(Operator, start)
			return
		}
	}
	l.i++
	l.emitAdvance(Error, start)
}

func (l *lexer) hasPrefix(s string) bool {
	if l.i+len(s) > len(l.src) {
		return false
	}
	for j := 0; j < len(s); j++ {
		if l.src[l.i+j] != s[j] {
			return false
		}
	}
	return true
}

func (l *lexer) isKeyword(b []byte) bool {
	var buf [64]byte
	if len(b) > len(buf) {
		return false
	}
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		buf[i] = c
	}
	_, ok := l.d.Keywords[string(buf[:len(b)])]
	return ok
}

func (l *lexer) isIdentOpen(c byte) bool {
	for _, q := range l.d.IdentQuote {
		if q.Open == c {
			return true
		}
	}
	return false
}

func (l *lexer) isStrOpen(c byte) bool {
	for _, q := range l.d.StrQuote {
		if q.Open == c {
			return true
		}
	}
	return false
}

func (l *lexer) isIdentClose(c byte) bool {
	for _, q := range l.d.IdentQuote {
		if q.Close == c {
			return true
		}
	}
	return false
}

func (l *lexer) identClose(open byte) byte {
	for _, q := range l.d.IdentQuote {
		if q.Open == open {
			return q.Close
		}
	}
	return open
}

var operators = []string{
	"->>", "#>>", "!~*", "~~*", "::", "<=", ">=", "<>", "!=", "||", "&&",
	"->", "#>", ":=", "=>", "==", "!~", "~*", "~~", "+", "-", "*", "/",
	"%", "=", "<", ">", "!", "~", "&", "|", "^", "\\",
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHex(c byte) bool   { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}
func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '$' }

func indexBytes(hay, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Significant returns the non-trivia tokens.
func Significant(tokens []Token) []Token {
	out := make([]Token, 0, len(tokens))
	for _, t := range tokens {
		if t.IsSignificant() {
			out = append(out, t)
		}
	}
	return out
}
