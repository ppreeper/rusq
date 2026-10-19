package lexer_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/source"
)

func TestLexKinds(t *testing.T) {
	src := source.New("t.sql", []byte("SELECT a, 'x''y', 1.5e3 FROM t -- c\nWHERE b >= 2;"))
	toks, errs := lexer.Lex(src, dialect.MustLookup("ansi"))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := map[lexer.Kind]int{lexer.Keyword: 3, lexer.Identifier: 3, lexer.String: 1, lexer.Number: 2, lexer.Comment: 1}
	got := map[lexer.Kind]int{}
	for _, tok := range toks {
		got[tok.Kind]++
	}
	if got[lexer.Keyword] != want[lexer.Keyword] {
		t.Errorf("keywords = %d, want %d", got[lexer.Keyword], want[lexer.Keyword])
	}
	if got[lexer.String] != 1 || got[lexer.Comment] != 1 {
		t.Errorf("strings=%d comments=%d", got[lexer.String], got[lexer.Comment])
	}
}

func TestQuotedIdentifiers(t *testing.T) {
	cases := []struct {
		dialect string
		sql     string
		kind    lexer.Kind
	}{
		{"ansi", `SELECT "col"`, lexer.QuotedIdentifier},
		{"mysql", "SELECT `col`", lexer.QuotedIdentifier},
		{"tsql", "SELECT [col]", lexer.QuotedIdentifier},
		{"bigquery", `SELECT "lit"`, lexer.String},
	}
	for _, tc := range cases {
		toks, _ := lexer.Lex(source.New("t", []byte(tc.sql)), dialect.MustLookup(tc.dialect))
		found := false
		for _, tok := range toks {
			if tok.Kind == tc.kind {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no %v token in %q", tc.dialect, tc.kind, tc.sql)
		}
	}
}

func TestDollarQuote(t *testing.T) {
	toks, errs := lexer.Lex(source.New("t", []byte("SELECT $tag$hi$tag$;")), dialect.MustLookup("postgres"))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	found := false
	for _, tok := range toks {
		if tok.Kind == lexer.String && tok.Text == "$tag$hi$tag$" {
			found = true
		}
	}
	if !found {
		t.Fatal("dollar-quoted string not lexed")
	}
}
