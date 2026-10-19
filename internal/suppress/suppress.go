// Package suppress parses rusq/noqa suppression directives from SQL comments.
package suppress

import (
	"strings"

	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/source"
)

// Kind is the directive type.
type Kind int

const (
	Ignore     Kind = iota // ignore a single line
	Disable                // begin a disabled range
	Enable                 // end a disabled range
	FileIgnore             // disable the whole file
	FmtOff                 // stop formatting
	FmtOn                  // resume formatting
	Skip                   // skip the next statement
)

// Directive is one parsed suppression.
type Directive struct {
	Kind    Kind
	Codes   []string
	All     bool
	Span    source.Range
	Line    int
	Text    string
	Used    bool
	Invalid []string
}

// Parse scans comments for directives.
func Parse(tokens []lexer.Token) []Directive {
	var out []Directive
	for _, t := range tokens {
		if t.Kind != lexer.Comment {
			continue
		}
		if d, ok := parseComment(t); ok {
			out = append(out, d)
		}
	}
	return out
}

func parseComment(t lexer.Token) (Directive, bool) {
	body := t.Text
	if strings.HasPrefix(body, "--") {
		body = body[2:]
	} else if strings.HasPrefix(body, "/*") {
		body = strings.TrimSuffix(body[2:], "*/")
	}
	body = strings.TrimSpace(body)
	lower := strings.ToLower(body)

	var rest string
	switch {
	case strings.HasPrefix(lower, "rusq:"):
		rest = strings.TrimSpace(body[len("rusq:"):])
	case strings.HasPrefix(lower, "noqa:"):
		rest = strings.TrimSpace(body[len("noqa:"):])
	case strings.EqualFold(body, "noqa"):
		rest = ""
	default:
		return Directive{}, false
	}

	d := Directive{Kind: Ignore, Span: t.Span, Line: t.Line, Text: body}
	lowerRest := strings.ToLower(rest)
	switch {
	case strings.HasPrefix(lowerRest, "fmt off"):
		d.Kind = FmtOff
		return d, true
	case strings.HasPrefix(lowerRest, "fmt on"):
		d.Kind = FmtOn
		return d, true
	case strings.HasPrefix(lowerRest, "file-ignore"):
		d.Kind = FileIgnore
		rest = strings.TrimSpace(strings.TrimPrefix(lowerRest, "file-ignore"))
		rest = strings.TrimPrefix(rest, "=")
	case strings.HasPrefix(lowerRest, "disable"):
		d.Kind = Disable
		rest = strings.TrimSpace(strings.TrimPrefix(lowerRest, "disable"))
		rest = strings.TrimPrefix(rest, "=")
	case strings.HasPrefix(lowerRest, "enable"):
		d.Kind = Enable
		rest = strings.TrimSpace(strings.TrimPrefix(lowerRest, "enable"))
		rest = strings.TrimPrefix(rest, "=")
	case strings.HasPrefix(lowerRest, "skip"):
		d.Kind = Skip
		rest = strings.TrimSpace(strings.TrimPrefix(lowerRest, "skip"))
		rest = strings.TrimPrefix(rest, "=")
	case strings.HasPrefix(lowerRest, "ignore"):
		d.Kind = Ignore
		rest = strings.TrimSpace(strings.TrimPrefix(lowerRest, "ignore"))
		rest = strings.TrimPrefix(rest, "=")
	}
	for _, c := range strings.FieldsFunc(rest, func(r rune) bool {
		return r == ',' || r == '|' || r == ' ' || r == '\t'
	}) {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.EqualFold(c, "all") {
			d.All = true
			continue
		}
		d.Codes = append(d.Codes, c)
	}
	if rest == "" {
		d.All = true
	}
	return d, true
}

// MatchesCode reports whether the directive covers code. Empty code lists with
// All set cover everything.
func (d Directive) MatchesCode(code string) bool {
	if d.All {
		return true
	}
	for _, c := range d.Codes {
		if strings.EqualFold(c, code) {
			return true
		}
	}
	return false
}
