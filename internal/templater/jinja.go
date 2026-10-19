package templater

import (
	"bytes"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/flosch/pongo2/v6"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

type jinjaTemplater struct{}

func (jinjaTemplater) Render(src source.Text, s settings.Settings) ([]Variant, []diagnostic.SyntaxError) {
	cfg := s.TemplaterConfig["jinja"]
	if truthy(cfg["ignore-templating"]) || truthy(cfg["ignore_templating"]) {
		return []Variant{substitute(src)}, nil
	}
	slices := Scan(src.Bytes)
	if len(slices) == 0 {
		return []Variant{identity(src)}, nil
	}
	onlyLiteral := true
	for _, sl := range slices {
		if sl.Kind != Literal {
			onlyLiteral = false
			break
		}
	}
	if onlyLiteral {
		return []Variant{identity(src)}, nil
	}

	instrumented := instrument(src.Bytes, slices)
	ctx, defined := jinjaContext(s, cfg)
	limit := s.RenderVariantLimit
	if limit <= 0 {
		limit = 5
	}

	out, err := execute(instrumented, ctx)
	if err != nil {
		v := substitute(src)
		return []Variant{v}, []diagnostic.SyntaxError{{
			Span:    source.Range{Start: 0, End: len(src.Bytes)},
			Message: "templating failed; using substitution mode: " + err.Error(),
		}}
	}
	variants := []Variant{buildFromMarkers(out, slices, "primary")}
	seen := map[string]bool{out: true}

	// A common dbt branch is {% if is_incremental() %}; render its true arm
	// as an additional variant.
	if bytes.Contains(src.Bytes, []byte("is_incremental")) && len(variants) < limit {
		alt := cloneContext(ctx)
		alt["is_incremental"] = func() bool { return true }
		if o, err := execute(instrumented, alt); err == nil && !seen[o] {
			seen[o] = true
			variants = append(variants, buildFromMarkers(o, slices, "if:is_incremental"))
		}
	}

	for _, name := range undefinedCandidates(src.Bytes, defined) {
		if len(variants) >= limit {
			break
		}
		alt := cloneContext(ctx)
		alt[name] = "x"
		o, err := execute(instrumented, alt)
		if err != nil || seen[o] {
			continue
		}
		seen[o] = true
		variants = append(variants, buildFromMarkers(o, slices, "var:"+name))
	}
	return variants, nil
}

func execute(tpl string, ctx pongo2.Context) (string, error) {
	t, err := pongo2.FromString(tpl)
	if err != nil {
		return "", err
	}
	return t.Execute(ctx)
}

const markerPrefix = "\x00RUSQ"

func marker(i int) string {
	return markerPrefix + strconv.Itoa(i) + "\x00"
}

func instrument(src []byte, slices []Slice) string {
	var b strings.Builder
	b.Grow(len(src) + len(slices)*10)
	for i, sl := range slices {
		b.Write(src[sl.SourceSpan.Start:sl.SourceSpan.End])
		b.WriteString(marker(i))
	}
	return b.String()
}

// buildFromMarkers removes markers from out and attributes each rendered
// segment to the slice that preceded it.
func buildFromMarkers(out string, slices []Slice, prov string) Variant {
	raw := []byte(out)
	var cleaned bytes.Buffer
	cleaned.Grow(len(raw))
	var parts []Part
	cursor := 0
	for cursor < len(raw) {
		idx, start, end, ok := nextMarker(raw, cursor)
		if !ok {
			break
		}
		seg := raw[cursor:start]
		rs := source.Range{Start: cleaned.Len(), End: cleaned.Len() + len(seg)}
		cleaned.Write(seg)
		if idx >= 0 && idx < len(slices) {
			sl := slices[idx]
			exact := sl.Kind == Literal && rs.Len() == sl.SourceSpan.Len()
			parts = append(parts, Part{
				Slice:    Slice{Kind: sl.Kind, SourceSpan: sl.SourceSpan, RenderedSpan: rs},
				Rendered: rs,
				Exact:    exact,
			})
		}
		cursor = end
	}
	if cursor < len(raw) {
		rs := source.Range{Start: cleaned.Len(), End: cleaned.Len() + len(raw) - cursor}
		cleaned.Write(raw[cursor:])
		parts = append(parts, Part{
			Slice:    Slice{Kind: Literal, SourceSpan: source.Range{Start: 0, End: 0}, RenderedSpan: rs},
			Rendered: rs,
		})
	}
	var sl []Slice
	for _, p := range parts {
		sl = append(sl, p.Slice)
	}
	return Variant{Rendered: cleaned.Bytes(), Slices: sl, Parts: parts, Provenance: []string{prov}}
}

func nextMarker(b []byte, from int) (idx, start, end int, ok bool) {
	p := bytes.Index(b[from:], []byte(markerPrefix))
	if p < 0 {
		return 0, 0, 0, false
	}
	start = from + p
	j := start + len(markerPrefix)
	k := j
	for k < len(b) && b[k] >= '0' && b[k] <= '9' {
		k++
	}
	if k == j || k >= len(b) || b[k] != 0 {
		return 0, 0, 0, false
	}
	n, _ := strconv.Atoi(string(b[j:k]))
	return n, start, k + 1, true
}

func jinjaContext(s settings.Settings, cfg map[string]any) (pongo2.Context, map[string]bool) {
	ctx := pongo2.Context{}
	defined := map[string]bool{}
	if c, ok := cfg["context"].(map[string]any); ok {
		for k, v := range c {
			ctx[k] = v
			defined[k] = true
		}
	}
	applyDBT := true
	if v, ok := cfg["apply-dbt-builtins"]; ok {
		applyDBT = truthy(v)
	} else if v, ok := cfg["apply_dbt_builtins"]; ok {
		applyDBT = truthy(v)
	}
	if applyDBT {
		ctx["ref"] = func(name string) string { return name }
		ctx["source"] = func(schema, table string) string { return schema + "_" + table }
		ctx["var"] = func(name string, def ...any) any {
			if v, ok := ctx[name]; ok {
				return v
			}
			if len(def) > 0 {
				return def[0]
			}
			return name
		}
		ctx["config"] = func(args ...any) string { return "" }
		ctx["is_incremental"] = func() bool { return false }
		ctx["this"] = "this"
		ctx["target"] = map[string]any{"name": "dev", "schema": "main", "type": "duckdb"}
		ctx["adapter"] = map[string]any{}
		for _, k := range []string{"ref", "source", "var", "config", "is_incremental", "this", "target", "adapter"} {
			defined[k] = true
		}
	}
	return ctx, defined
}

func cloneContext(in pongo2.Context) pongo2.Context {
	out := pongo2.Context{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	case int64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

var jinjaKeywords = map[string]bool{
	"if": true, "else": true, "elif": true, "endif": true, "for": true, "in": true,
	"endfor": true, "not": true, "and": true, "or": true, "true": true, "false": true,
	"none": true, "is": true, "macro": true, "endmacro": true, "set": true, "endset": true,
	"block": true, "endblock": true, "extends": true, "include": true, "import": true,
	"from": true, "with": true, "endwith": true, "raw": true, "endraw": true, "filter": true,
	"endfilter": true, "call": true, "endcall": true, "autoescape": true, "endautoescape": true,
	"do": true, "break": true, "continue": true, "loop": true, "recursive": true,
	"materialization": true, "endmaterialization": true,
}

// undefinedCandidates finds simple identifiers used in tags that are neither
// defined in context nor declared by the template.
func undefinedCandidates(src []byte, defined map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	declared := map[string]bool{}
	// Declarations: {% set x = ... %} and {% for x in ... %}.
	declRe := regexp.MustCompile(`\{%-?\s*(?:set|for)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	for _, m := range declRe.FindAllSubmatch(src, -1) {
		declared[string(m[1])] = true
	}
	for _, sl := range Scan(src) {
		if sl.Kind == Literal || sl.Kind == Comment {
			continue
		}
		tag := src[sl.SourceSpan.Start:sl.SourceSpan.End]
		visible := blankQuoted(tag)
		for _, loc := range identRe.FindAllIndex(visible, -1) {
			name := string(visible[loc[0]:loc[1]])
			if jinjaKeywords[name] || defined[name] || declared[name] || seen[name] {
				continue
			}
			// Skip attribute access and function names.
			if loc[0] > 0 && tag[loc[0]-1] == '.' {
				continue
			}
			if loc[1] < len(tag) && tag[loc[1]] == '(' {
				continue
			}
			// Skip module/function-looking names that start the tag.
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// blankQuoted replaces quoted string contents with spaces so identifiers in
// literals are not treated as template variables.
func blankQuoted(tag []byte) []byte {
	out := append([]byte(nil), tag...)
	var quote byte
	for i := 0; i < len(out); i++ {
		c := out[i]
		if quote != 0 {
			if c == '\\' {
				out[i] = ' '
				if i+1 < len(out) {
					out[i+1] = ' '
					i++
				}
				continue
			}
			if c == quote {
				quote = 0
				continue
			}
			out[i] = ' '
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		}
	}
	return out
}
