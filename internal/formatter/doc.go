package formatter

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// docKind enumerates the document IR combinators. The IR follows the
// Wadler/Prettier "document" model: a tree of layout instructions that a
// width-aware printer renders to text. Groups flatten onto one line when they
// fit within the configured line length and break otherwise.
type docKind uint8

const (
	kText        docKind = iota // literal text
	kConcat                     // concatenation of children
	kLine                       // space when flat, newline when broken
	kSoftLine                   // nothing when flat, newline when broken
	kHardLine                   // always a newline (forces enclosing groups to break)
	kLiteralLine                // newline with no indentation (blank lines)
	kGroup                      // try flat, fall back to broken
	kIndent                     // increase indentation for the child
	kDedent                     // decrease indentation for the child
	kIfBreak                    // children[0] when broken, children[1] when flat
	kFill                       // treated as concat (reserved)
	kLineSuffix                 // content flushed at the next newline
	kBreakParent                // forces the enclosing group to break
	kAlign                      // align newlines to the current column (see align)
)

// doc is a node in the document IR.
type doc struct {
	kind     docKind
	text     string
	children []*doc

	// brk is computed by propagateBreaks: true when the group must break.
	brk bool
}

func text(s string) *doc { return &doc{kind: kText, text: s} }

func concat(ds ...*doc) *doc { return &doc{kind: kConcat, children: compact(ds)} }

func line() *doc        { return &doc{kind: kLine} }
func softline() *doc    { return &doc{kind: kSoftLine} }
func hardline() *doc    { return &doc{kind: kHardLine} }
func literalline() *doc { return &doc{kind: kLiteralLine} }

func group(d *doc) *doc {
	if d == nil {
		return nil
	}
	return &doc{kind: kGroup, children: []*doc{d}}
}

func indent(d *doc) *doc {
	if d == nil {
		return nil
	}
	return &doc{kind: kIndent, children: []*doc{d}}
}

func dedent(d *doc) *doc {
	if d == nil {
		return nil
	}
	return &doc{kind: kDedent, children: []*doc{d}}
}

func ifBreak(b, f *doc) *doc {
	return &doc{kind: kIfBreak, children: []*doc{b, f}}
}

func lineSuffix(d *doc) *doc {
	if d == nil {
		return nil
	}
	return &doc{kind: kLineSuffix, children: []*doc{d}}
}

// align aligns the newlines inside d to the column just past the current
// position, which is where the separating space preceding d would be written in
// flat mode. It is used by align-following to line continuation rows up under
// the first item of a clause body.
func align(d *doc) *doc {
	if d == nil {
		return nil
	}
	return &doc{kind: kAlign, children: []*doc{d}}
}

func compact(ds []*doc) []*doc {
	out := make([]*doc, 0, len(ds))
	for _, d := range ds {
		if d == nil {
			continue
		}
		if d.kind == kConcat {
			out = append(out, d.children...)
			continue
		}
		out = append(out, d)
	}
	return out
}

// propagateBreaks marks groups that contain a hard line break (directly or via
// a nested forced group) so the printer knows they cannot flatten. Groups that
// break only because of width are not propagated to their parents.
func propagateBreaks(d *doc) bool {
	if d == nil {
		return false
	}
	switch d.kind {
	case kHardLine, kLiteralLine, kBreakParent:
		return true
	case kGroup:
		forced := propagateAll(d.children)
		if forced {
			d.brk = true
		}
		return d.brk
	default:
		return propagateAll(d.children)
	}
}

func propagateAll(ds []*doc) bool {
	forced := false
	for _, c := range ds {
		if propagateBreaks(c) {
			forced = true
		}
	}
	return forced
}

type printMode uint8

const (
	modeBreak printMode = iota
	modeFlat
)

type command struct {
	indent int
	align  int // absolute column for newline padding; -1 = use indent units
	mode   printMode
	d      *doc
}

func cmd(indent int, mode printMode, d *doc) command {
	return command{indent: indent, align: -1, mode: mode, d: d}
}

func cmdAlign(indent, align int, mode printMode, d *doc) command {
	return command{indent: indent, align: align, mode: mode, d: d}
}

// sub returns a command for a child document, preserving the alignment context
// established by an enclosing kAlign.
func (c command) sub(indent int, mode printMode, d *doc) command {
	return command{indent: indent, align: c.align, mode: mode, d: d}
}

// render prints a document IR to text at the given width and indentation unit.
func render(d *doc, width int, unit string) string {
	if d == nil {
		return ""
	}
	if width <= 0 {
		width = 88
	}
	propagateBreaks(d)

	unitWidth := runewidth.StringWidth(unit)
	var out strings.Builder
	pos := 0
	var suffixes []*doc

	stack := []command{cmd(0, modeBreak, d)}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch c.d.kind {
		case kText:
			out.WriteString(c.d.text)
			pos += runewidth.StringWidth(c.d.text)
		case kConcat, kFill:
			for i := len(c.d.children) - 1; i >= 0; i-- {
				stack = append(stack, c.sub(c.indent, c.mode, c.d.children[i]))
			}
		case kIndent:
			stack = append(stack, c.sub(c.indent+1, c.mode, c.d.children[0]))
		case kDedent:
			ind := c.indent - 1
			if ind < 0 {
				ind = 0
			}
			stack = append(stack, c.sub(ind, c.mode, c.d.children[0]))
		case kGroup:
			if c.d.brk {
				stack = append(stack, c.sub(c.indent, modeBreak, c.d.children[0]))
				break
			}
			if c.mode == modeFlat {
				stack = append(stack, c.sub(c.indent, modeFlat, c.d.children[0]))
				break
			}
			rest := make([]command, 0, len(stack)+1)
			rest = append(rest, stack...)
			rest = append(rest, c.sub(c.indent, modeFlat, c.d.children[0]))
			if fits(width-pos, rest) {
				stack = append(stack, c.sub(c.indent, modeFlat, c.d.children[0]))
			} else {
				stack = append(stack, c.sub(c.indent, modeBreak, c.d.children[0]))
			}
		case kIfBreak:
			if c.mode == modeBreak {
				stack = append(stack, c.sub(c.indent, c.mode, c.d.children[0]))
			} else {
				stack = append(stack, c.sub(c.indent, c.mode, c.d.children[1]))
			}
		case kLine:
			if c.mode == modeFlat {
				out.WriteByte(' ')
				pos++
			} else {
				writeNewline(&out, unit, c.indent, c.align, unitWidth, &pos, &suffixes, &stack)
			}
		case kSoftLine:
			if c.mode == modeBreak {
				writeNewline(&out, unit, c.indent, c.align, unitWidth, &pos, &suffixes, &stack)
			}
		case kHardLine:
			writeNewline(&out, unit, c.indent, c.align, unitWidth, &pos, &suffixes, &stack)
		case kLiteralLine:
			writeNewline(&out, "", 0, -1, 0, &pos, &suffixes, &stack)
		case kAlign:
			// The +1 accounts for the separating space that the child's leading
			// line emits when the group is flat.
			stack = append(stack, cmdAlign(c.indent, pos+1, c.mode, c.d.children[0]))
		case kLineSuffix:
			suffixes = append(suffixes, c.d.children[0])
		case kBreakParent:
			// no output
		}
	}
	flushSuffixes(&out, &suffixes, &pos)
	return out.String()
}

// writeNewline emits a newline plus indentation. Any pending line suffixes are
// printed first, then re-queued after the newline command so they resume.
func writeNewline(out *strings.Builder, unit string, ind, align, unitWidth int, pos *int, suffixes *[]*doc, stack *[]command) {
	if len(*suffixes) > 0 {
		// Re-run the newline after printing the suffixes.
		*stack = append(*stack, cmdAlign(ind, align, modeBreak, &doc{kind: kHardLine}))
		for i := len(*suffixes) - 1; i >= 0; i-- {
			*stack = append(*stack, cmd(ind, modeFlat, (*suffixes)[i]))
		}
		*suffixes = nil
		return
	}
	trimTrailingSpaces(out)
	out.WriteByte('\n')
	if align >= 0 {
		out.WriteString(strings.Repeat(" ", align))
		*pos = align
	} else if ind > 0 && unit != "" {
		s := strings.Repeat(unit, ind)
		out.WriteString(s)
		*pos = ind * unitWidth
	} else {
		*pos = 0
	}
}

func flushSuffixes(out *strings.Builder, suffixes *[]*doc, pos *int) {
	for _, d := range *suffixes {
		out.WriteString(flatText(d))
		*pos += runewidth.StringWidth(flatText(d))
	}
	*suffixes = nil
}

// flatText renders a doc without any breaks; used for line suffixes.
func flatText(d *doc) string {
	var b strings.Builder
	var walk func(*doc)
	walk = func(d *doc) {
		if d == nil {
			return
		}
		switch d.kind {
		case kText:
			b.WriteString(d.text)
		case kConcat, kFill, kGroup, kIndent, kDedent, kAlign:
			for _, c := range d.children {
				walk(c)
			}
		case kLine:
			b.WriteByte(' ')
		case kIfBreak:
			walk(d.children[1])
		case kLineSuffix:
			walk(d.children[0])
		}
	}
	walk(d)
	return b.String()
}

func trimTrailingSpaces(out *strings.Builder) {
	s := out.String()
	i := len(s)
	for i > 0 && (s[i-1] == ' ' || s[i-1] == '\t') {
		i--
	}
	if i != len(s) {
		out.Reset()
		out.WriteString(s[:i])
	}
}

// fits reports whether the next commands fit on the current line before a
// forced or soft break.
func fits(remaining int, stack []command) bool {
	for remaining >= 0 && len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch c.d.kind {
		case kText:
			remaining -= runewidth.StringWidth(c.d.text)
		case kConcat, kFill:
			for i := len(c.d.children) - 1; i >= 0; i-- {
				stack = append(stack, cmd(c.indent, c.mode, c.d.children[i]))
			}
		case kIndent, kDedent, kGroup, kLineSuffix, kAlign:
			stack = append(stack, cmd(c.indent, c.mode, c.d.children[0]))
		case kIfBreak:
			if c.mode == modeBreak {
				stack = append(stack, cmd(c.indent, c.mode, c.d.children[0]))
			} else {
				stack = append(stack, cmd(c.indent, c.mode, c.d.children[1]))
			}
		case kLine:
			if c.mode == modeFlat {
				remaining--
			} else {
				return true
			}
		case kSoftLine:
			if c.mode == modeBreak {
				return true
			}
		case kHardLine, kLiteralLine:
			return true
		}
	}
	return remaining >= 0
}
