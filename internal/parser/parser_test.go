package parser_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/source"
)

var roundTripCases = []struct {
	name    string
	dialect string
	sql     string
}{
	{"simple", "ansi", "SELECT a, b FROM t WHERE a = 1;\n"},
	{"cte", "ansi", "WITH x AS (SELECT 1) SELECT * FROM x;\n"},
	{"union", "ansi", "SELECT 1 UNION ALL SELECT 2;\n"},
	{"join", "ansi", "SELECT a.id FROM a LEFT JOIN b ON a.id = b.id;\n"},
	{"comments", "ansi", "-- lead\nSELECT /* mid */ a -- tail\nFROM t;\n"},
	{"strings", "ansi", "SELECT 'it''s', \"quoted\" FROM t;\n"},
	{"postgres", "postgres", "SELECT $tag$body$tag$, x::int FROM t;\n"},
	{"mysql", "mysql", "SELECT `col` FROM `tbl` LIMIT 10;\n"},
	{"bigquery", "bigquery", "SELECT * EXCEPT (a) FROM `p.d.t`;\n"},
	{"tsql", "tsql", "SELECT TOP 10 [col] FROM [dbo].[t];\n"},
	{"malformed", "ansi", "SELECT 'unterminated\n"},
	{"empty", "ansi", ""},
	{"only-comment", "ansi", "-- just a comment\n"},
	{"crlf", "ansi", "SELECT 1;\r\nSELECT 2;\r\n"},
	{"semicolons", "ansi", ";;SELECT 1;;\n"},
}

func TestRoundTrip(t *testing.T) {
	for _, tc := range roundTripCases {
		t.Run(tc.name, func(t *testing.T) {
			src := source.New("t.sql", []byte(tc.sql))
			d := dialect.MustLookup(tc.dialect)
			toks, _ := lexer.Lex(src, d)
			tr, _ := parser.New().Parse(toks, d)
			got := string(parser.Unparse(tr))
			if got != tc.sql {
				t.Fatalf("round-trip mismatch\nwant: %q\n got: %q", tc.sql, got)
			}
		})
	}
}

func TestStatementKinds(t *testing.T) {
	cases := map[string]string{
		"SELECT 1;":                 "select",
		"INSERT INTO t VALUES (1);": "insert",
		"UPDATE t SET a = 1;":       "update",
		"DELETE FROM t;":            "delete",
		"CREATE TABLE t (a int);":   "create",
	}
	for sql, want := range cases {
		toks, _ := lexer.Lex(source.New("t", []byte(sql)), dialect.MustLookup("ansi"))
		tr, _ := parser.New().Parse(toks, dialect.MustLookup("ansi"))
		if len(tr.Root.Children) == 0 {
			t.Fatalf("%q: no statements", sql)
		}
		if got := tr.Root.Children[0].Kind.String(); got != want {
			t.Errorf("%q: kind = %s, want %s", sql, got, want)
		}
	}
}
