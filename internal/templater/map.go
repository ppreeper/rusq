package templater

import (
	"github.com/ppreeper/rusq/internal/diagnostic"
)

// MapDiagnostic maps a diagnostic from rendered coordinates to source
// coordinates. Fixes that touch a templated region are downgraded to
// display-only.
func (v *Variant) MapDiagnostic(d diagnostic.Diagnostic) diagnostic.Diagnostic {
	span, exact := v.MapSpan(d.Span.Start, d.Span.End)
	d.Span = span
	if d.Fix == nil {
		return d
	}
	if !exact {
		d.Fix = nil
		d.Fixability = diagnostic.Display
		return d
	}
	edits := make([]diagnostic.Edit, 0, len(d.Fix.Edits))
	for _, e := range d.Fix.Edits {
		es, ok := v.MapSpan(e.Span.Start, e.Span.End)
		if !ok || es.Len() != e.Span.Len() {
			d.Fix = nil
			d.Fixability = diagnostic.Display
			return d
		}
		edits = append(edits, diagnostic.Edit{Span: es, Replacement: e.Replacement})
	}
	d.Fix = &diagnostic.Fix{Edits: edits}
	return d
}

// MapError maps a rendered syntax error to a source range.
func (v *Variant) MapError(e diagnostic.SyntaxError) diagnostic.SyntaxError {
	span, _ := v.MapSpan(e.Span.Start, e.Span.End)
	return diagnostic.SyntaxError{Span: span, Message: e.Message}
}
