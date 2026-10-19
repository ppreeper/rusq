package linter_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/selector"
	"github.com/ppreeper/rusq/internal/settings"
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

func BenchmarkLintDefault(b *testing.B) {
	s := settings.Default()
	l := linter.New(s)
	src := source.New("bench.sql", []byte(benchSQL))
	b.ReportAllocs()
	b.SetBytes(int64(len(benchSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.Run(src, linter.Options{})
	}
}

// fixSQL carries several safe fixes: NULL comparisons, a redundant alias and a
// trailing-comma-free list that LT01 would touch.
const fixSQL = `SELECT a AS a, b
FROM t
WHERE a = NULL AND b <> NULL AND c = NULL
ORDER BY a;
`

// BenchmarkLintAll runs every catalogued rule against one file — the worst case
// a user can ask for.
func BenchmarkLintAll(b *testing.B) {
	s := settings.Default()
	s.Set = selector.Set{Include: []string{"ALL"}}
	l := linter.New(s)
	src := source.New("bench.sql", []byte(benchSQL))
	b.ReportAllocs()
	b.SetBytes(int64(len(benchSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.Run(src, linter.Options{})
	}
}

// BenchmarkFix measures the whole fix loop, which re-lints after each round of
// edits until the file stops changing.
func BenchmarkFix(b *testing.B) {
	s := settings.Default()
	l := linter.New(s)
	src := source.New("bench.sql", []byte(fixSQL))
	if res := l.Run(src, linter.Options{Fix: true}); !res.Changed {
		b.Fatal("benchmark input has no fixable findings")
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(fixSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.Run(src, linter.Options{Fix: true})
	}
}
