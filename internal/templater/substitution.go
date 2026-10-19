package templater

import (
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

type rawTemplater struct{}

func (rawTemplater) Render(src source.Text, _ settings.Settings) ([]Variant, []diagnostic.SyntaxError) {
	return []Variant{identity(src)}, nil
}

type replacement struct {
	start, end int
	value      byte
	zeroWidth  bool
}

// buildVariant assembles a rendered variant from a source file and a set of
// byte-range replacements.
func buildVariant(src source.Text, reps []replacement, provenance []string) Variant {
	b := make([]byte, 0, len(src.Bytes))
	var parts []Part
	cursor := 0
	for _, r := range reps {
		if r.start < cursor || r.end < r.start || r.end > len(src.Bytes) {
			continue
		}
		if r.start > cursor {
			lit := source.Range{Start: cursor, End: r.start}
			rs := source.Range{Start: len(b), End: len(b) + lit.Len()}
			b = append(b, src.Bytes[cursor:r.start]...)
			parts = append(parts, Part{
				Slice:    Slice{Kind: Literal, SourceSpan: lit, RenderedSpan: rs},
				Rendered: rs,
				Exact:    true,
			})
		}
		var repl []byte
		if !r.zeroWidth {
			repl = neutralize(src.Bytes[r.start:r.end], r.value)
		}
		srcSpan := source.Range{Start: r.start, End: r.end}
		rs := source.Range{Start: len(b), End: len(b) + len(repl)}
		b = append(b, repl...)
		parts = append(parts, Part{
			Slice:    Slice{Kind: Templated, SourceSpan: srcSpan, RenderedSpan: rs},
			Rendered: rs,
			Exact:    false,
		})
		cursor = r.end
	}
	if cursor < len(src.Bytes) {
		lit := source.Range{Start: cursor, End: len(src.Bytes)}
		rs := source.Range{Start: len(b), End: len(b) + lit.Len()}
		b = append(b, src.Bytes[cursor:]...)
		parts = append(parts, Part{
			Slice:    Slice{Kind: Literal, SourceSpan: lit, RenderedSpan: rs},
			Rendered: rs,
			Exact:    true,
		})
	}
	var slices []Slice
	for _, p := range parts {
		slices = append(slices, p.Slice)
	}
	return Variant{Rendered: b, Slices: slices, Parts: parts, Provenance: provenance}
}

// neutralize blanks a region while preserving newlines, placing val at the
// first non-newline position.
func neutralize(b []byte, val byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if c == '\n' || c == '\r' {
			out[i] = c
		} else {
			out[i] = ' '
		}
	}
	if val != 0 {
		for i := range out {
			if out[i] == ' ' {
				out[i] = val
				break
			}
		}
	}
	return out
}

// substitute is the non-executing fallback: template tags are blanked,
// expressions become a single token, and all branches remain visible.
func substitute(src source.Text) Variant {
	var reps []replacement
	for _, sl := range Scan(src.Bytes) {
		switch sl.Kind {
		case Literal:
			continue
		case Templated:
			reps = append(reps, replacement{start: sl.SourceSpan.Start, end: sl.SourceSpan.End, value: 'x'})
		default:
			reps = append(reps, replacement{start: sl.SourceSpan.Start, end: sl.SourceSpan.End, value: 0})
		}
	}
	return buildVariant(src, reps, []string{"substitution"})
}
