package ast_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
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

// BenchmarkASTProject measures the typed AST projection over an already parsed
// CST (SPEC §12.2): the projection must never re-parse.
func BenchmarkASTProject(b *testing.B) {
	d := dialect.MustLookup("ansi")
	toks, _ := lexer.Lex(source.New("bench.sql", []byte(benchSQL)), d)
	tr, _ := parser.New().Parse(toks, d)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ast.Project(tr)
	}
}
