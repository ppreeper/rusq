// Package source defines the shared text, position, and range primitives used
// by every stage of rusq. Offsets are byte offsets into a Text; positions are
// 1-based line/column pairs derived from a Locator.
package source

import "bytes"

// Text is a named piece of source. Path is the logical path (the
// --stdin-filename value when reading from stdin).
type Text struct {
	Path  string
	Bytes []byte
}

// New returns a Text for the given path and bytes.
func New(path string, b []byte) Text { return Text{Path: path, Bytes: b} }

// String returns the source as a string.
func (t Text) String() string { return string(t.Bytes) }

// Len returns the number of bytes.
func (t Text) Len() int { return len(t.Bytes) }

// Pos is a 1-based line/column position.
type Pos struct {
	Line int
	Col  int
}

// Range is a half-open byte range [Start, End).
type Range struct {
	Start int
	End   int
}

// NewRange returns a Range.
func NewRange(start, end int) Range { return Range{Start: start, End: end} }

// NewRangeAt returns a zero-width Range at off.
func NewRangeAt(off int) Range { return Range{Start: off, End: off} }

// Len returns the number of bytes in the range.
func (r Range) Len() int {
	if r.End <= r.Start {
		return 0
	}
	return r.End - r.Start
}

// Empty reports whether the range covers no bytes.
func (r Range) Empty() bool { return r.End <= r.Start }

// Contains reports whether off is inside the range.
func (r Range) Contains(off int) bool { return off >= r.Start && off < r.End }

// Overlaps reports whether two ranges share any bytes.
func (r Range) Overlaps(o Range) bool { return r.Start < o.End && o.Start < r.End }

// Shift returns the range moved by delta bytes.
func (r Range) Shift(delta int) Range { return Range{Start: r.Start + delta, End: r.End + delta} }

// Locator maps byte offsets to line/column positions. It is built once per
// file and is safe for concurrent reads.
type Locator struct {
	src        []byte
	lineStarts []int
	lineEnds   []int
}

// NewLocator indexes the line boundaries of src.
func NewLocator(src []byte) *Locator {
	l := &Locator{src: src}
	l.lineStarts = append(l.lineStarts, 0)
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			l.lineEnds = append(l.lineEnds, i)
			l.lineStarts = append(l.lineStarts, i+1)
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				continue
			}
			l.lineEnds = append(l.lineEnds, i)
			l.lineStarts = append(l.lineStarts, i+1)
		}
	}
	l.lineEnds = append(l.lineEnds, len(src))
	return l
}

// Position converts a byte offset to a 1-based line/column.
func (l *Locator) Position(off int) Pos {
	if off < 0 {
		off = 0
	}
	if off > len(l.src) {
		off = len(l.src)
	}
	lo, hi := 0, len(l.lineStarts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if l.lineStarts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return Pos{Line: lo + 1, Col: off - l.lineStarts[lo] + 1}
}

// Offset converts a 1-based line/column to a byte offset, clamped to the file.
func (l *Locator) Offset(p Pos) int {
	if p.Line < 1 {
		return 0
	}
	if p.Line > len(l.lineStarts) {
		return len(l.src)
	}
	off := l.lineStarts[p.Line-1] + p.Col - 1
	if off < 0 {
		return 0
	}
	if off > len(l.src) {
		return len(l.src)
	}
	return off
}

// LineCount returns the number of lines in the file.
func (l *Locator) LineCount() int { return len(l.lineStarts) }

// LineStart returns the byte offset where the 1-based line begins.
func (l *Locator) LineStart(line int) int {
	if line < 1 {
		return 0
	}
	if line > len(l.lineStarts) {
		return len(l.src)
	}
	return l.lineStarts[line-1]
}

// LineEnd returns the byte offset just past the last byte of the 1-based line,
// excluding the line terminator.
func (l *Locator) LineEnd(line int) int {
	if line < 1 || line > len(l.lineEnds) {
		return len(l.src)
	}
	return l.lineEnds[line-1]
}

// LineRange returns the byte range of the 1-based line, excluding the
// terminator.
func (l *Locator) LineRange(line int) Range {
	return Range{Start: l.LineStart(line), End: l.LineEnd(line)}
}

// LineText returns the text of the 1-based line, excluding the terminator.
func (l *Locator) LineText(line int) []byte {
	r := l.LineRange(line)
	if r.Start > len(l.src) {
		return nil
	}
	return l.src[r.Start:r.End]
}

// Src returns the indexed source.
func (l *Locator) Src() []byte { return l.src }

// CountLines counts logical lines the way an editor does: a trailing newline
// does not create an extra line.
func CountLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}
