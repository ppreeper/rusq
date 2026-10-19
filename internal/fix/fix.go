// Package fix applies diagnostics' edits to source text.
package fix

import (
	"sort"

	"github.com/ppreeper/rusq/internal/diagnostic"
)

// Applied records a diagnostic whose edits were applied.
type Applied struct {
	Diagnostic diagnostic.Diagnostic
}

// Apply returns src with all eligible, non-overlapping edits applied. Edits are
// sorted by span start and applied from the end so offsets stay valid.
func Apply(src []byte, ds []diagnostic.Diagnostic, allowUnsafe bool) ([]byte, []diagnostic.Diagnostic) {
	type candidate struct {
		edit diagnostic.Edit
		d    diagnostic.Diagnostic
	}
	var cands []candidate
	for _, d := range ds {
		if d.Fix == nil || len(d.Fix.Edits) == 0 {
			continue
		}
		switch d.Fixability {
		case diagnostic.Safe:
		case diagnostic.Unsafe:
			if !allowUnsafe {
				continue
			}
		default:
			continue
		}
		for _, e := range d.Fix.Edits {
			cands = append(cands, candidate{edit: e, d: d})
		}
	}
	if len(cands) == 0 {
		return src, nil
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].edit.Span.Start != cands[j].edit.Span.Start {
			return cands[i].edit.Span.Start < cands[j].edit.Span.Start
		}
		return cands[i].edit.Span.End < cands[j].edit.Span.End
	})

	// Drop overlapping edits: keep the first by start offset.
	var kept []candidate
	lastEnd := -1
	var applied []diagnostic.Diagnostic
	seen := map[string]bool{}
	for _, c := range cands {
		if c.edit.Span.Start < lastEnd {
			continue
		}
		kept = append(kept, c)
		if c.edit.Span.End > lastEnd {
			lastEnd = c.edit.Span.End
		}
		key := c.d.Code + "|" + c.d.Message
		if !seen[key] {
			seen[key] = true
			applied = append(applied, c.d)
		}
	}

	out := append([]byte(nil), src...)
	for i := len(kept) - 1; i >= 0; i-- {
		e := kept[i].edit
		if e.Span.Start < 0 || e.Span.End > len(out) || e.Span.Start > e.Span.End {
			continue
		}
		next := make([]byte, 0, len(out)-(e.Span.End-e.Span.Start)+len(e.Replacement))
		next = append(next, out[:e.Span.Start]...)
		next = append(next, e.Replacement...)
		next = append(next, out[e.Span.End:]...)
		out = next
	}
	return out, applied
}
