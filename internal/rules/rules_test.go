package rules_test

import (
	"strings"
	"testing"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

func lint(t *testing.T, sql string) []diagnostic.Diagnostic {
	t.Helper()
	s := settings.Default()
	l := linter.New(s)
	return l.Run(source.New("t.sql", []byte(sql)), linter.Options{}).Diagnostics
}

func codes(ds []diagnostic.Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return out
}

func hasCode(ds []diagnostic.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func lintAll(t *testing.T, sql string) []diagnostic.Diagnostic {
	t.Helper()
	s := settings.Default()
	s.Set.Include = []string{"ALL"}
	l := linter.New(s)
	return l.Run(source.New("t.sql", []byte(sql)), linter.Options{}).Diagnostics
}

func TestRules(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"is-null", "SELECT a FROM t WHERE b = NULL;", "CV05"},
		{"union", "SELECT 1 UNION SELECT 2;", "AM02"},
		{"duplicate-alias", "SELECT a.x FROM t AS a JOIN u AS a ON a.x = a.y;", "AL04"},
		{"derived-alias", "SELECT * FROM (SELECT 1);", "AL10"},
		{"unused-cte", "WITH c AS (SELECT 1) SELECT 2;", "ST03"},
		{"unknown-columns", "SELECT * FROM a UNION SELECT * FROM b;", "AM04"},
		{"invalid-directive", "SELECT 1; -- rusq: ZZ99", "RUS101"},
		{"consecutive-semicolons", "SELECT 1;;", "ST12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lint(t, tc.sql)
			if !hasCode(got, tc.want) {
				t.Fatalf("sql %q: codes = %v, want %s", tc.sql, codes(got), tc.want)
			}
		})
	}
}

func TestStyleRules(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"else-null", "SELECT CASE WHEN a THEN 1 ELSE NULL END FROM t;", "ST01"},
		{"coalesce", "SELECT IFNULL(a, 0) FROM t;", "CV02"},
		{"not-equal", "SELECT a FROM t WHERE b != 1 AND c != 2 AND d <> 3;", "CV01"},
		{"unquoted", `SELECT "plain" FROM t;`, "RF06"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lintAll(t, tc.sql)
			if !hasCode(got, tc.want) {
				t.Fatalf("sql %q: codes = %v, want %s", tc.sql, codes(got), tc.want)
			}
		})
	}
}

func TestSuppressions(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		code string
	}{
		{"line-ignore", "SELECT a FROM t WHERE b = NULL; -- rusq: CV05", "CV05"},
		{"line-noqa", "SELECT a FROM t WHERE b = NULL; -- noqa: CV05", "CV05"},
		{"disable-range", "-- rusq: disable=CV05\nSELECT a FROM t WHERE b = NULL;\n-- rusq: enable=all\n", "CV05"},
		{"file-ignore", "-- rusq: file-ignore=CV05\nSELECT a FROM t WHERE b = NULL;", "CV05"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lint(t, tc.sql); hasCode(got, tc.code) {
				t.Fatalf("suppression did not apply; codes = %v", codes(got))
			}
		})
	}
}

func TestFixEngine(t *testing.T) {
	s := settings.Default()
	s.Set.Include = []string{"ALL"}
	l := linter.New(s)
	src := source.New("t.sql", []byte("SELECT a FROM t WHERE b = NULL\n"))
	res := l.Run(src, linter.Options{Fix: true})
	if !res.Changed {
		t.Fatal("expected fixes")
	}
	got := string(res.FixedSource)
	if !strings.Contains(got, "IS NULL") {
		t.Errorf("expected IS NULL, got %q", got)
	}
	if !strings.HasSuffix(got, ";\n") {
		t.Errorf("expected terminator and newline, got %q", got)
	}
}

func TestJJ01(t *testing.T) {
	s := settings.Default()
	s.Templater = "jinja"
	s.Set.Include = []string{"ALL"}
	l := linter.New(s)
	sql := "SELECT {{ref('x')}} FROM t;\n"
	ds := l.Run(source.New("t.sql", []byte(sql)), linter.Options{}).Diagnostics
	if !hasCode(ds, "JJ01") {
		t.Fatalf("expected JJ01, got %v", codes(ds))
	}

	raw := settings.Default()
	raw.Set.Include = []string{"ALL"}
	ds = linter.New(raw).Run(source.New("t.sql", []byte(sql)), linter.Options{}).Diagnostics
	if hasCode(ds, "JJ01") {
		t.Fatalf("JJ01 fired without the jinja templater: %v", codes(ds))
	}
}

func TestParserDiagnostic(t *testing.T) {
	got := lint(t, "SELECT 'unterminated\n")
	if !hasCode(got, "RUS104") {
		t.Fatalf("expected RUS104, got %v", codes(got))
	}
}

func lintIn(t *testing.T, dialectName string, all bool, sql string) []diagnostic.Diagnostic {
	t.Helper()
	s := settings.Default()
	d, ok := dialect.Lookup(dialectName)
	if !ok {
		t.Fatalf("unknown dialect %q", dialectName)
	}
	s.Dialect, s.DialectName = d, d.Name
	if all {
		s.Set.Include = []string{"ALL"}
	}
	l := linter.New(s)
	return l.Run(source.New("t.sql", []byte(sql)), linter.Options{}).Diagnostics
}

func TestNewDefaultRules(t *testing.T) {
	cases := []struct {
		name    string
		dialect string
		sql     string
		want    string
	}{
		{"am07", "ansi", "SELECT a, b FROM t UNION SELECT c FROM u;", "AM07"},
		{"rf01", "ansi", "SELECT z.y FROM t AS x;", "RF01"},
		{"rf02", "ansi", "SELECT a FROM t JOIN u ON t.id = u.id;", "RF02"},
		{"rf07", "ansi", "SELECT SUM(x) OVER (PARTITION BY total) AS total FROM t;", "RF07"},
		{"st10", "ansi", "SELECT a FROM t WHERE 1 = 1;", "ST10"},
		{"st11", "ansi", "SELECT a FROM t CROSS JOIN u;", "ST11"},
		{"pg01", "postgres", "LOCK TABLE t IN ACCESS EXCLUSIVE MODE;", "PG01"},
		{"pg02", "postgres", "ALTER TABLE t ADD CONSTRAINT fk FOREIGN KEY (a) REFERENCES u(id);", "PG02"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lintIn(t, tc.dialect, false, tc.sql)
			if !hasCode(got, tc.want) {
				t.Fatalf("codes = %v, want %s", codes(got), tc.want)
			}
		})
	}
}

func TestNewStyleRules(t *testing.T) {
	cases := []struct {
		name    string
		dialect string
		sql     string
		want    string
	}{
		{"al03", "ansi", "SELECT a + b FROM t;", "AL03"},
		{"am05", "ansi", "SELECT a FROM t JOIN u ON t.id = u.id;", "AM05"},
		{"am06", "ansi", "SELECT a FROM t GROUP BY 1, b;", "AM06"},
		{"cv04", "ansi", "SELECT COUNT(1) FROM t;", "CV04"},
		{"cv07", "ansi", "(SELECT 1);", "CV07"},
		{"cv10", "bigquery", `SELECT 'a', 'b', "c" FROM t;`, "CV10"},
		{"cv11", "ansi", "SELECT CAST(a AS int), b::int FROM t;", "CV11"},
		{"cv12", "ansi", "SELECT a FROM t JOIN u ON t.id = u.id WHERE t.x = u.y;", "CV12"},
		{"lt06", "ansi", "SELECT COUNT (a) FROM t;", "LT06"},
		{"rf04", "ansi", `SELECT "select" FROM t;`, "RF04"},
		{"rf05", "ansi", `SELECT "has space" FROM t;`, "RF05"},
		{"st05", "ansi", "SELECT a FROM (SELECT 1) x;", "ST05"},
		{"st07", "ansi", "SELECT a FROM t JOIN u USING (id);", "ST07"},
		{"tq04", "tsql", "SELECT alias = a FROM t;", "TQ04"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lintIn(t, tc.dialect, true, tc.sql)
			if !hasCode(got, tc.want) {
				t.Fatalf("codes = %v, want %s", codes(got), tc.want)
			}
		})
	}
}

func TestCV09BlockedWords(t *testing.T) {
	s := settings.Default()
	s.Set.Include = []string{"ALL"}
	s.Rules = map[string]map[string]any{
		"convention.blocked_words": {"blocked-words": []any{"deprecated_col"}},
	}
	src := source.New("t.sql", []byte("SELECT deprecated_col FROM t;"))
	got := linter.New(s).Run(src, linter.Options{}).Diagnostics
	if !hasCode(got, "CV09") {
		t.Fatalf("codes = %v, want CV09", codes(got))
	}
}
