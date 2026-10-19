package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

// ST14 — a view defined in the project but referenced nowhere.
//
// Only query objects (views) are reported: a table is commonly created by a
// migration and consumed by application code outside the SQL corpus, so
// reporting unused tables would be noise. Duplicates are left to ST13.
type st14 struct{ meta Meta }

func (r *st14) Meta() Meta { return r.meta }

func (r *st14) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	ix := ctx.Index
	if ix == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, def := range ix.DefinitionsInPath(ctx.Source.Path) {
		if def.Kind != "view" {
			continue
		}
		if ix.IsReferenced(def.Name) {
			continue
		}
		if len(ix.DefinitionFiles(def.Name)) > 1 {
			continue // ST13 owns duplicates
		}
		out = append(out, diag(r.meta, source.Range{Start: def.Start, End: def.End},
			"View "+def.Name+" is defined but never referenced in the project."))
	}
	return out
}

// RF08 — a templated reference to an object the project never defines.
//
// The rule reads the RAW source, because the renderer resolves ref()/source()
// away before the SQL ever reaches the parser. A single-argument call is looked
// up under its own name; a two-argument source(schema, table) is looked up
// under the joined schema_table name, matching how the dbt-style builtins
// resolve it. It is silent unless the index holds at least one definition, so a
// project that keeps its models outside the SQL corpus is not flooded.
type rf08 struct{ meta Meta }

func (r *rf08) Meta() Meta { return r.meta }

func (r *rf08) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	ix := ctx.Index
	if ix == nil || ix.DefinitionCount() == 0 {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, call := range findMacroCalls(ctx.Source.Bytes) {
		if len(ix.DefinitionFiles(call.Name)) > 0 {
			continue
		}
		out = append(out, diag(r.meta, call.Span,
			"Reference to unknown object "+call.Name+"; no file in the project defines it."))
	}
	return out
}

// macroCall is a ref()/source() call found in raw source.
type macroCall struct {
	Kind string // "ref" or "source"
	Name string // resolved lookup name
	Span source.Range
}

// findMacroCalls scans raw bytes for ref('x') and source('a', 'b') calls.
func findMacroCalls(b []byte) []macroCall {
	var out []macroCall
	for i := 0; i < len(b); i++ {
		kind, n := macroKeyword(b, i)
		if kind == "" {
			continue
		}
		j := skipSpace(b, i+n)
		if j >= len(b) || b[j] != '(' {
			continue
		}
		j = skipSpace(b, j+1)
		var args []string
		var start, end int
		for {
			if j >= len(b) || (b[j] != '\'' && b[j] != '"') {
				break
			}
			q := b[j]
			s := j
			j++
			for j < len(b) && b[j] != q {
				j++
			}
			if j >= len(b) {
				break
			}
			args = append(args, string(b[s+1:j]))
			start, end = s, j+1
			j++
			k := skipSpace(b, j)
			if k < len(b) && b[k] == ',' {
				j = skipSpace(b, k+1)
				continue
			}
			break
		}
		if len(args) > 0 {
			out = append(out, macroCall{
				Kind: kind,
				Name: strings.ToLower(strings.Join(args, "_")),
				Span: source.Range{Start: start, End: end},
			})
			i = j
		}
	}
	return out
}

// macroKeyword reports the macro keyword starting at i, if any.
func macroKeyword(b []byte, i int) (string, int) {
	if wordAt(b, i, "ref") {
		return "ref", 3
	}
	if wordAt(b, i, "source") {
		return "source", 6
	}
	return "", 0
}

// wordAt reports whether the ASCII word w (lower-case) starts at i on a word
// boundary.
func wordAt(b []byte, i int, w string) bool {
	if i+len(w) > len(b) {
		return false
	}
	for k := 0; k < len(w); k++ {
		if b[i+k]|0x20 != w[k] {
			return false
		}
	}
	if i > 0 && isWordByte(b[i-1]) {
		return false
	}
	if i+len(w) < len(b) && isWordByte(b[i+len(w)]) {
		return false
	}
	return true
}

func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

func init() {
	Register(&st14{meta: newMeta("ST14", "structure.unused_definition", "structure", Style, Index, diagnostic.NoFix,
		"Views defined in the project should be referenced somewhere.", "force-enable")})
	Register(&rf08{meta: newMeta("RF08", "references.unknown_object", "references", Style, Index, diagnostic.NoFix,
		"Templated references should resolve to an object the project defines.", "force-enable")})
}
