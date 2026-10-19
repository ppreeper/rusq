// Package templater renders templated SQL into one or more lintable variants.
// It implements the design in docs/TEMPLATING.md: pre-scan slices, traced
// rendering, variant enumeration, and a non-executing substitution fallback.
package templater

import (
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

// SliceKind classifies one pre-scanned source slice.
type SliceKind int

const (
	Literal SliceKind = iota
	Templated
	BlockStart
	BlockMid
	BlockEnd
	Comment
)

// Slice is a source-side (and optionally rendered-side) region.
type Slice struct {
	Kind         SliceKind
	SourceSpan   source.Range
	RenderedSpan source.Range
}

// Part maps one rendered region back to a source slice.
type Part struct {
	Slice    Slice
	Rendered source.Range
	// Exact is true when rendered offsets map linearly onto the source span
	// (untouched literal text).
	Exact bool
}

// Variant is one rendering of a template.
type Variant struct {
	Rendered   []byte
	Slices     []Slice
	Parts      []Part
	Provenance []string
}

// MapOffset maps a rendered byte offset to a source offset. The second result
// reports whether the mapping is exact (the offset lies in unmodified literal
// text).
func (v *Variant) MapOffset(off int) (int, bool) {
	if off < 0 {
		off = 0
	}
	if off > len(v.Rendered) {
		off = len(v.Rendered)
	}
	// Find the last part starting at or before off.
	lo, hi := 0, len(v.Parts)
	for lo < hi {
		mid := (lo + hi) / 2
		if v.Parts[mid].Rendered.Start <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return 0, false
	}
	p := v.Parts[lo-1]
	if p.Exact && off >= p.Rendered.Start && off <= p.Rendered.End {
		delta := off - p.Rendered.Start
		if delta <= p.Slice.SourceSpan.Len() {
			return p.Slice.SourceSpan.Start + delta, true
		}
	}
	return p.Slice.SourceSpan.Start, false
}

// MapSpan maps a rendered span to a source span.
func (v *Variant) MapSpan(start, end int) (source.Range, bool) {
	s, okS := v.MapOffset(start)
	e, okE := v.MapOffset(end)
	if e < s {
		e = s
	}
	return source.Range{Start: s, End: e}, okS && okE
}

// Templater renders a source file.
type Templater interface {
	Render(source.Text, settings.Settings) ([]Variant, []diagnostic.SyntaxError)
}

// New returns the templater for name. Unknown names fall back to raw; dbt and
// sqlmesh degrade to jinja.
func New(name string) Templater {
	switch name {
	case "placeholder":
		return placeholderTemplater{}
	case "jinja", "dbt", "sqlmesh":
		return jinjaTemplater{}
	case "python":
		return pythonTemplater{}
	default:
		return rawTemplater{}
	}
}

// identity returns a pass-through variant.
func identity(src source.Text) Variant {
	r := source.Range{Start: 0, End: len(src.Bytes)}
	return Variant{
		Rendered:   src.Bytes,
		Slices:     []Slice{{Kind: Literal, SourceSpan: r, RenderedSpan: r}},
		Parts:      []Part{{Slice: Slice{Kind: Literal, SourceSpan: r}, Rendered: r, Exact: true}},
		Provenance: []string{"raw"},
	}
}
