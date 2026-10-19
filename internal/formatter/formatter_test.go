package formatter_test

import (
	"strings"
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/formatter"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

func TestFormatIdempotent(t *testing.T) {
	cases := []string{
		"SELECT 1;\n",
		"SELECT 1;   \n\n\n",
		"select 1;\r\n",
		"  \n\nSELECT 1;\n",
		"",
	}
	f := formatter.New(settings.Default())
	for _, in := range cases {
		once := f.FormatBytes([]byte(in))
		twice := f.FormatBytes(once)
		if string(once) != string(twice) {
			t.Errorf("not idempotent for %q: %q then %q", in, once, twice)
		}
	}
}

func TestFormatNormalizes(t *testing.T) {
	f := formatter.New(settings.Default())
	out := f.FormatBytes([]byte("SELECT 1;   \r\n\r\n\r\n"))
	if string(out) != "SELECT 1;\n" {
		t.Fatalf("got %q", out)
	}
}

func TestFormatRange(t *testing.T) {
	f := formatter.New(settings.Default())
	f.StartLine, f.EndLine = 2, 2
	in := "SELECT 1;   \nSELECT 2;   \n"
	out := f.FormatBytes([]byte(in))
	if string(out) != "SELECT 1;   \nSELECT 2;\n" {
		t.Fatalf("got %q", out)
	}
}

// sigTokens returns the significant token texts of src.
func sigTokens(src string) []string {
	d := dialect.MustLookup("ansi")
	toks, _ := lexer.Lex(source.Text{Bytes: []byte(src)}, d)
	var out []string
	for _, tk := range toks {
		if tk.IsSignificant() {
			out = append(out, tk.Text)
		}
	}
	return out
}

func TestFormatPreservesTokens(t *testing.T) {
	inputs := []string{
		"SELECT 1;\n",
		"select a,b from t where x=1 and y=2;\n",
		"INSERT INTO t (a, b) VALUES (1, 2), (3, 4);\n",
		"UPDATE t SET a=1, b=2 WHERE id=3;\n",
		"DELETE FROM t WHERE id = 5;\n",
		"CREATE TABLE x (id int primary key, name text not null);\n",
		"WITH c AS (SELECT 1) SELECT * FROM c;\n",
		"select case when a>1 then 'x' else 'y' end as l from t;\n",
		"select * from a union all select * from b order by 1;\n",
		"SELECT\n  id,\n  -- comment\n  name\nFROM users;\n",
		"select 'a''b', 1.5e3, $1, :name, ?, now() - interval '1 day' from t;\n",
		// malformed: exercises the conservative fallback
		"select (1;\n",
	}
	f := formatter.New(settings.Default())
	for _, in := range inputs {
		out := string(f.FormatBytes([]byte(in)))
		want := sigTokens(in)
		got := sigTokens(out)
		if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
			t.Errorf("token stream changed for %q:\n want %v\n got  %v", in, want, got)
		}
	}
}

func TestFormatBreaksLongLists(t *testing.T) {
	f := formatter.New(settings.Default())
	in := "SELECT aaaaaaaaaaaa, bbbbbbbbbbbb, cccccccccccc, dddddddddddd, eeeeeeeeeeee, ffffffffffff FROM t;\n"
	out := string(f.FormatBytes([]byte(in)))
	want := "SELECT\n    aaaaaaaaaaaa,\n    bbbbbbbbbbbb,\n    cccccccccccc,\n    dddddddddddd,\n    eeeeeeeeeeee,\n    ffffffffffff\nFROM t;\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatIndentUnit(t *testing.T) {
	s := settings.Default()
	s.Format.IndentUnit = "tab"
	f := formatter.New(s)
	in := "SELECT aaaaaaaaaaaa, bbbbbbbbbbbb, cccccccccccc, dddddddddddd, eeeeeeeeeeee, ffffffffffff FROM t;\n"
	out := string(f.FormatBytes([]byte(in)))
	if !strings.Contains(out, "\n\taaaaaaaaaaaa,") {
		t.Fatalf("expected tab indentation, got:\n%s", out)
	}
}

func TestFormatLineLength(t *testing.T) {
	s := settings.Default()
	s.Format.LineLength = 20
	f := formatter.New(s)
	out := string(f.FormatBytes([]byte("SELECT aaaa, bbbb, cccc FROM t;\n")))
	if !strings.Contains(out, "\n    aaaa,") {
		t.Fatalf("expected breaking at short line length, got:\n%s", out)
	}
}

func TestFormatCreateTable(t *testing.T) {
	f := formatter.New(settings.Default())
	in := "create table x (id int primary key, name text not null, created_at timestamp default now());\n"
	out := string(f.FormatBytes([]byte(in)))
	want := "create table x (\n    id int primary key,\n    name text not null,\n    created_at timestamp default now()\n);\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatTrailingComment(t *testing.T) {
	f := formatter.New(settings.Default())
	in := "select id, -- keep\nname from users;\n"
	out := string(f.FormatBytes([]byte(in)))
	if !strings.Contains(out, "-- keep\n") {
		t.Fatalf("comment lost or misplaced:\n%s", out)
	}
	if strings.Contains(out, "\n\n") {
		t.Fatalf("unexpected blank line after comment:\n%s", out)
	}
}

var corpus = []string{
	"SELECT 1;",
	"select a, b, c from t where a = 1 and b = 2 or c = 3;",
	"SELECT DISTINCT a, b FROM t GROUP BY a, b HAVING count(*) > 1 ORDER BY a DESC LIMIT 10 OFFSET 5;",
	"WITH x AS (SELECT 1 AS n), y AS (SELECT 2 AS m) SELECT * FROM x JOIN y ON x.n = y.m;",
	"INSERT INTO t (a, b) VALUES (1, 2), (3, 4);",
	"INSERT INTO t SELECT * FROM u;",
	"UPDATE t SET a = 1, b = 2 WHERE id = 3;",
	"DELETE FROM t WHERE id = 5;",
	"CREATE TABLE t (id INT PRIMARY KEY, name TEXT NOT NULL, ts TIMESTAMP DEFAULT now());",
	"ALTER TABLE t ADD COLUMN x INT;",
	"DROP TABLE IF EXISTS t;",
	"SELECT CASE WHEN a > 1 THEN 'x' WHEN a = 1 THEN 'y' ELSE 'z' END AS label FROM t;",
	"SELECT count(*), sum(x), avg(y) FROM t;",
	"SELECT a.*, b.id FROM a JOIN b ON a.id = b.a_id LEFT JOIN c ON c.id = b.c_id;",
	"SELECT * FROM a UNION ALL SELECT * FROM b INTERSECT SELECT * FROM c;",
	"SELECT (SELECT max(z) FROM y) AS m, x FROM t;",
	"SELECT * FROM t WHERE x IN (1, 2, 3) AND y BETWEEN 1 AND 10;",
	"SELECT coalesce(a, b, 0) FROM t;",
	"SELECT x::int, CAST(y AS text) FROM t;",
	"select 'it''s', 1.5e3, -42, 0xFF from t;",
	"SELECT /* block */ a FROM t;",
	"SELECT a, -- trailing\n b FROM t;",
	"-- leading\nSELECT 1;",
	"SELECT a FROM t;\n\n\nSELECT b FROM u;",
	"SELECT\n  a\n  ,b\n  ,c\nFROM t;",
	"SELECT a FROM t WHERE EXISTS (SELECT 1 FROM u WHERE u.id = t.id);",
	"SELECT * FROM t ORDER BY 1, 2;",
	"SELECT * FROM t1 JOIN t2 USING (id);",
	"SELECT a AS \"quoted alias\" FROM t;",
	"SELECT lag(x) OVER (PARTITION BY g ORDER BY ts) FROM t;",
	// malformed inputs must not panic and must round-trip through the fallback
	"SELECT (1;",
	"select from;",
	"((((((",
	"",
	"\n\n\n",
}

func TestFormatCorpusAllDialects(t *testing.T) {
	for _, name := range dialect.Names() {
		d := dialect.MustLookup(name)
		s := settings.Default()
		s.DialectName = name
		s.Dialect = d
		f := formatter.New(s)
		for _, in := range corpus {
			if in == "" {
				continue
			}
			out := f.FormatBytes([]byte(in))
			// idempotency
			twice := f.FormatBytes(out)
			if string(out) != string(twice) {
				t.Fatalf("[%s] not idempotent for %q:\n%q\n%q", name, in, out, twice)
			}
			// token preservation
			want := sigTokensD(in, d)
			got := sigTokensD(string(out), d)
			if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
				t.Fatalf("[%s] token stream changed for %q:\n want %v\n got  %v", name, in, want, got)
			}
		}
	}
}

func TestFormatCommaStyleLeading(t *testing.T) {
	s := settings.Default()
	s.Format.CommaStyle = "leading"
	f := formatter.New(s)
	in := "SELECT aaaaaaaaaaaa, bbbbbbbbbbbb, cccccccccccc, dddddddddddd, eeeeeeeeeeee, ffffffffffff FROM t;\n"
	out := string(f.FormatBytes([]byte(in)))
	want := "SELECT\n" +
		"    aaaaaaaaaaaa\n" +
		"    , bbbbbbbbbbbb\n" +
		"    , cccccccccccc\n" +
		"    , dddddddddddd\n" +
		"    , eeeeeeeeeeee\n" +
		"    , ffffffffffff\n" +
		"FROM t;\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func sigTokensD(src string, d *dialect.Dialect) []string {
	toks, _ := lexer.Lex(source.Text{Bytes: []byte(src)}, d)
	var out []string
	for _, tk := range toks {
		if tk.IsSignificant() {
			out = append(out, tk.Text)
		}
	}
	return out
}
