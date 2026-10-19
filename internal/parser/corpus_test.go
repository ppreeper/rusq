package parser_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/source"
)

const corpusSQL = `-- a representative statement
WITH recent AS (
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

func TestRoundTripAllDialects(t *testing.T) {
	for _, name := range dialect.Names() {
		t.Run(name, func(t *testing.T) {
			d := dialect.MustLookup(name)
			src := source.New("t.sql", []byte(corpusSQL))
			toks, _ := lexer.Lex(src, d)
			tr, _ := parser.New().Parse(toks, d)
			if got := string(parser.Unparse(tr)); got != corpusSQL {
				t.Fatalf("round-trip mismatch in %s", name)
			}
		})
	}
}
