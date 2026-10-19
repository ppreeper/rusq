package templater

import (
	"bytes"

	"github.com/ppreeper/rusq/internal/source"
)

// Scan splits src on Jinja delimiters. It never fails: an unterminated tag
// becomes a slice extending to the end of the file.
func Scan(src []byte) []Slice {
	var out []Slice
	i := 0
	litStart := 0
	for i < len(src) {
		open := -1
		kind := Templated
		closeTok := ""
		switch {
		case i+1 < len(src) && src[i] == '{' && src[i+1] == '{':
			open, closeTok = i, "}}"
		case i+1 < len(src) && src[i] == '{' && src[i+1] == '%':
			open, closeTok = i, "%}"
			kind = BlockStart
		case i+1 < len(src) && src[i] == '{' && src[i+1] == '#':
			open, closeTok = i, "#}"
			kind = Comment
		}
		if open < 0 {
			i++
			continue
		}
		if open > litStart {
			out = append(out, Slice{Kind: Literal, SourceSpan: source.Range{Start: litStart, End: open}})
		}
		end := findClose(src, open+2, closeTok)
		if end < 0 {
			end = len(src)
		} else {
			end += len(closeTok)
		}
		if kind == BlockStart {
			kind = classifyBlock(src[open:end])
		}
		out = append(out, Slice{Kind: kind, SourceSpan: source.Range{Start: open, End: end}})
		i = end
		litStart = end
	}
	if litStart < len(src) {
		out = append(out, Slice{Kind: Literal, SourceSpan: source.Range{Start: litStart, End: len(src)}})
	}
	return out
}

func findClose(src []byte, from int, tok string) int {
	idx := bytes.Index(src[from:], []byte(tok))
	if idx < 0 {
		return -1
	}
	return from + idx
}

func classifyBlock(tag []byte) SliceKind {
	inner := bytes.TrimSpace(tag)
	if len(inner) >= 2 && inner[0] == '{' && inner[1] == '%' {
		inner = inner[2:]
	}
	if len(inner) >= 2 && inner[len(inner)-2] == '%' && inner[len(inner)-1] == '}' {
		inner = inner[:len(inner)-2]
	}
	inner = bytes.TrimSpace(inner)
	for _, p := range bytes.Fields(inner) {
		switch string(p) {
		case "else", "elif":
			return BlockMid
		case "endif", "endfor", "endmacro", "endblock", "endcall", "endfilter", "endwith", "endset":
			return BlockEnd
		}
		break
	}
	return BlockStart
}
