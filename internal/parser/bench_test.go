package parser_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/source"
)

func BenchmarkParse(b *testing.B) {
	src := source.New("bench.sql", []byte(corpusSQL))
	d := dialect.MustLookup("ansi")
	toks, _ := lexer.Lex(src, d)
	p := parser.New()
	b.ReportAllocs()
	b.SetBytes(int64(len(corpusSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Parse(toks, d)
	}
}

func BenchmarkRoundTrip(b *testing.B) {
	src := source.New("bench.sql", []byte(corpusSQL))
	d := dialect.MustLookup("ansi")
	toks, _ := lexer.Lex(src, d)
	tr, _ := parser.New().Parse(toks, d)
	b.ReportAllocs()
	b.SetBytes(int64(len(corpusSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parser.Unparse(tr)
	}
}
