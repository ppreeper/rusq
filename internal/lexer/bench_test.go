package lexer_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
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
GROUP BY c.id, c.name
HAVING SUM(r.total) > 100
ORDER BY revenue DESC
LIMIT 25;
`

func BenchmarkLex(b *testing.B) {
	src := source.New("bench.sql", []byte(benchSQL))
	d := dialect.MustLookup("ansi")
	b.ReportAllocs()
	b.SetBytes(int64(len(benchSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lexer.Lex(src, d)
	}
}
