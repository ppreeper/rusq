package lint_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

const benchSQL = `WITH recent AS (
    SELECT id, customer_id, total
    FROM orders
    WHERE created_at >= CURRENT_DATE - INTERVAL '30' DAY
)
SELECT c.id, c.name, SUM(r.total) AS revenue
FROM customers AS c
LEFT JOIN recent AS r ON r.customer_id = c.id
WHERE c.active = TRUE
GROUP BY c.id, c.name
HAVING SUM(r.total) > 100
ORDER BY revenue DESC
LIMIT 25;
`

// BenchmarkLintLines measures the line-oriented view built for the Line pass.
func BenchmarkLintLines(b *testing.B) {
	d := dialect.MustLookup("ansi")
	src := source.New("bench.sql", []byte(benchSQL))
	toks, _ := lexer.Lex(src, d)
	loc := source.NewLocator(src.Bytes)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lint.NewLines(toks, loc)
	}
}
