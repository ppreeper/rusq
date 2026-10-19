// Package lint defines the read-only context handed to rules.
package lint

import (
	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// Context is the immutable state a rule may inspect.
type Context struct {
	Source   source.Text
	Locator  *source.Locator
	Dialect  *dialect.Dialect
	Settings settings.Settings
	Tokens   []lexer.Token
	Tree     *tree.Tree
	AST      *ast.File
	Lines    *Lines
	// Index is the project index, present only when cross-file analysis is
	// enabled (SPEC §8.3). Rules with an Index source must tolerate nil.
	Index *index.Index
}

// Significant returns the non-trivia tokens.
func (c *Context) Significant() []lexer.Token { return lexer.Significant(c.Tokens) }

// Line is one physical or logical line.
type Line struct {
	Number int
	Start  int
	End    int
	Text   string
	Tokens []lexer.Token
}

// Lines is the line-oriented view of a file.
type Lines struct {
	Physical []Line
	Logical  []Line
}

// NewLines builds the physical and logical line views.
func NewLines(tokens []lexer.Token, loc *source.Locator) *Lines {
	l := &Lines{}
	for n := 1; n <= loc.LineCount(); n++ {
		r := loc.LineRange(n)
		l.Physical = append(l.Physical, Line{
			Number: n,
			Start:  r.Start,
			End:    r.End,
			Text:   string(loc.LineText(n)),
		})
	}
	// Attach tokens to physical lines.
	for _, t := range tokens {
		if t.Kind == lexer.EOF {
			continue
		}
		if t.Line >= 1 && t.Line <= len(l.Physical) {
			l.Physical[t.Line-1].Tokens = append(l.Physical[t.Line-1].Tokens, t)
		}
	}
	// Logical lines are split at top-level semicolons.
	start := 0
	for i, t := range tokens {
		if t.Kind == lexer.Semicolon {
			l.Logical = append(l.Logical, logicalLine(tokens[start:i+1], loc))
			start = i + 1
		}
	}
	if start < len(tokens) {
		seg := tokens[start:]
		if hasSignificant(seg) {
			l.Logical = append(l.Logical, logicalLine(seg, loc))
		}
	}
	return l
}

func logicalLine(tokens []lexer.Token, loc *source.Locator) Line {
	first, last := -1, -1
	for i, t := range tokens {
		if t.Kind == lexer.EOF {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return Line{}
	}
	sp := tokens[first].Span
	ep := tokens[last].Span
	ln := Line{Start: sp.Start, End: ep.End, Text: string(loc.Src()[sp.Start:ep.End])}
	return ln
}

func hasSignificant(tokens []lexer.Token) bool {
	for _, t := range tokens {
		if t.IsSignificant() {
			return true
		}
	}
	return false
}
