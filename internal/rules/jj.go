package rules

import (
	"bytes"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

// JJ01 — single spaces inside Jinja tags.
type jj01 struct{ meta Meta }

func (r *jj01) Meta() Meta { return r.meta }

func (r *jj01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.Settings.Templater != "jinja" {
		return nil
	}
	b := ctx.Source.Bytes
	var out []diagnostic.Diagnostic
	for i := 0; i+2 < len(b); i++ {
		if b[i] != '{' {
			continue
		}
		var closeTok string
		switch b[i+1] {
		case '{':
			closeTok = "}}"
		case '%':
			closeTok = "%}"
		default:
			continue
		}
		rel := bytes.Index(b[i+2:], []byte(closeTok))
		if rel < 0 {
			continue
		}
		end := i + 2 + rel // start of the closing token
		if end <= i+2 {
			i = end + 1
			continue
		}
		first := b[i+2]
		last := b[end-1]
		leftOK := isPad(first) || first == '-' || first == '+'
		rightOK := isPad(last) || last == '-' || last == '+'
		if leftOK && rightOK {
			i = end + 1
			continue
		}
		span := source.Range{Start: i, End: end + 2}
		d := diag(r.meta, span, "Jinja tags should have a single space on either side.")
		var edits []diagnostic.Edit
		if !leftOK {
			edits = append(edits, edit(source.NewRangeAt(i+2), " "))
		}
		if !rightOK {
			edits = append(edits, edit(source.NewRangeAt(end), " "))
		}
		d.Fix = &diagnostic.Fix{Edits: edits}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
		i = end + 1
	}
	return out
}

func isPad(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func init() {
	Register(&jj01{meta: newMeta("JJ01", "jinja.padding", "jinja", Style, Token, diagnostic.Safe, "Jinja tags should have single whitespace on either side.")})
}
