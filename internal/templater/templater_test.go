package templater_test

import (
	"strings"
	"testing"

	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/templater"
)

func render(t *testing.T, name, sql string) ([]templater.Variant, int) {
	t.Helper()
	s := settings.Default()
	s.Templater = name
	vs, errs := templater.New(name).Render(source.New("t.sql", []byte(sql)), s)
	return vs, len(errs)
}

func TestScan(t *testing.T) {
	sql := "SELECT {{ a }} FROM {% if x %}t{% endif %} {# c #};"
	slices := templater.Scan([]byte(sql))
	if len(slices) != 9 {
		t.Fatalf("slices = %d: %#v", len(slices), slices)
	}
}

func TestRaw(t *testing.T) {
	vs, _ := render(t, "raw", "SELECT 1;\n")
	if len(vs) != 1 || string(vs[0].Rendered) != "SELECT 1;\n" {
		t.Fatalf("raw variant = %#v", vs)
	}
}

func TestSubstitutionKeepsBranchesAndLines(t *testing.T) {
	sql := "{% if x %}\nSELECT 1\n{% else %}\nSELECT 2\n{% endif %}\n"
	s := settings.Default()
	s.Templater = "jinja"
	s.TemplaterConfig = map[string]map[string]any{"jinja": {"ignore-templating": true}}
	vs, errs := templater.New("jinja").Render(source.New("t.sql", []byte(sql)), s)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	got := string(vs[0].Rendered)
	if !strings.Contains(got, "SELECT 1") || !strings.Contains(got, "SELECT 2") {
		t.Fatalf("branches not both visible: %q", got)
	}
	if strings.Count(got, "\n") != strings.Count(sql, "\n") {
		t.Fatalf("newlines not preserved: %q", got)
	}
}

func TestJinjaMapping(t *testing.T) {
	sql := "SELECT {{ 1 + 2 }} FROM t WHERE a = NULL;\n"
	vs, errs := render(t, "jinja", sql)
	if errs != 0 {
		t.Fatalf("errors: %d", errs)
	}
	v := vs[0]
	rendered := string(v.Rendered)
	if !strings.Contains(rendered, "SELECT 3 FROM t") {
		t.Fatalf("rendered = %q", rendered)
	}
	ri := strings.Index(rendered, "= NULL")
	si := strings.Index(sql, "= NULL")
	got, exact := v.MapOffset(ri)
	if !exact {
		t.Fatalf("mapping was not exact")
	}
	if got != si {
		t.Fatalf("mapped %d, want %d", got, si)
	}
}

func TestJinjaVariants(t *testing.T) {
	sql := "{% if flag %}SELECT 1{% else %}SELECT 2{% endif %}"
	vs, errs := render(t, "jinja", sql)
	if errs != 0 {
		t.Fatalf("errors: %d", errs)
	}
	if len(vs) < 2 {
		t.Fatalf("expected two variants, got %d", len(vs))
	}
	seen := map[string]bool{}
	for _, v := range vs {
		seen[string(v.Rendered)] = true
	}
	if !seen["SELECT 1"] || !seen["SELECT 2"] {
		t.Fatalf("variants = %v", seen)
	}
}

func TestJinjaIncrementalVariant(t *testing.T) {
	sql := "{% if is_incremental() %}SELECT 1{% else %}SELECT 2{% endif %}"
	vs, _ := render(t, "jinja", sql)
	if len(vs) < 2 {
		t.Fatalf("expected incremental variant, got %d", len(vs))
	}
}

func TestJinjaFallback(t *testing.T) {
	vs, errs := render(t, "jinja", "{% if %}SELECT 1")
	if errs == 0 {
		t.Fatal("expected a templating error")
	}
	if len(vs) != 1 || !strings.Contains(string(vs[0].Rendered), "SELECT 1") {
		t.Fatalf("fallback variant = %#v", vs)
	}
}

func TestPlaceholder(t *testing.T) {
	s := settings.Default()
	s.Templater = "placeholder"
	s.TemplaterConfig = map[string]map[string]any{"placeholder": {"param-style": "question_mark"}}
	sql := "SELECT a FROM t WHERE b = ? AND c = ?;\n"
	vs, _ := templater.New("placeholder").Render(source.New("t", []byte(sql)), s)
	if len(vs) != 1 {
		t.Fatalf("variants = %d", len(vs))
	}
	if len(vs[0].Rendered) != len(sql) {
		t.Fatalf("placeholder replacement changed length")
	}
	if strings.Count(string(vs[0].Rendered), "?") != 0 {
		t.Fatalf("placeholder not replaced: %q", vs[0].Rendered)
	}
}
