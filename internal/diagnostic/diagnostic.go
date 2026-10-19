// Package diagnostic defines the finding model shared by rules, the linter,
// and every output renderer.
package diagnostic

import (
	"fmt"

	"github.com/ppreeper/rusq/internal/source"
)

// Severity ranks a diagnostic.
type Severity int

const (
	Error Severity = iota
	Warning
	Hint
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	case Hint:
		return "hint"
	}
	return "unknown"
}

// Fixability describes how safe a fix is.
type Fixability int

const (
	NoFix Fixability = iota
	Safe
	Unsafe
	Display
)

func (f Fixability) String() string {
	switch f {
	case Safe:
		return "safe"
	case Unsafe:
		return "unsafe"
	case Display:
		return "display"
	}
	return "none"
}

// Edit replaces Span with Replacement.
type Edit struct {
	Span        source.Range
	Replacement string
}

// Fix is an ordered set of non-overlapping edits.
type Fix struct {
	Edits []Edit
}

// Related is a secondary span attached to a diagnostic.
type Related struct {
	Span    source.Range
	Message string
}

// Diagnostic is a single finding.
type Diagnostic struct {
	Code       string
	Name       string
	Severity   Severity
	Span       source.Range
	Message    string
	Help       string
	Fix        *Fix
	Fixability Fixability
	Provenance []string
	Related    []Related
}

// SyntaxError is a recoverable parse failure. It is reported like a
// diagnostic (RUS104) but never aborts a run.
type SyntaxError struct {
	Span    source.Range
	Message string
}

func (e SyntaxError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Span.Start, e.Span.End, e.Message)
}

// Sort orders diagnostics deterministically by start, end, and code.
func Sort(ds []Diagnostic) {
	sortSlice(ds, func(a, b Diagnostic) bool {
		if a.Span.Start != b.Span.Start {
			return a.Span.Start < b.Span.Start
		}
		if a.Span.End != b.Span.End {
			return a.Span.End < b.Span.End
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
}

// sortSlice is a tiny insertion sort; diagnostic lists are short per file and
// avoiding reflection keeps the hot path cheap.
func sortSlice[T any](s []T, less func(a, b T) bool) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && less(s[j], s[j-1]); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
