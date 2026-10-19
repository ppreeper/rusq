package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/walk"
)

const benchFileSQL = `WITH recent AS (
    SELECT id, customer_id, total
    FROM orders
    WHERE created_at >= CURRENT_DATE - INTERVAL '30' DAY
)
SELECT c.id, c.name, SUM(r.total) AS revenue
FROM customers AS c
LEFT JOIN recent AS r ON r.customer_id = c.id
WHERE c.active = TRUE AND c.deleted_at = NULL
GROUP BY c.id, c.name
ORDER BY revenue DESC
LIMIT 25;
`

// corpusFiles writes n files and returns them as discovery results.
func corpusFiles(b *testing.B, n int) []walk.File {
	b.Helper()
	dir := b.TempDir()
	files := make([]walk.File, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.sql", i))
		if err := os.WriteFile(p, []byte(benchFileSQL), 0o644); err != nil {
			b.Fatal(err)
		}
		files = append(files, walk.File{Path: p})
	}
	return files
}

// BenchmarkStartup measures constructing the command tree, resolving config and
// turning it into settings — the work done before any file is read.
func BenchmarkStartup(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rusq.toml"), []byte("dialect = \"postgres\"\n[format]\nindent-width = 2\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewRootCmd()
		c, _, err := loadConfig(dir)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := settings.FromConfig(c, dir, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckFiles measures the cold end-to-end pipeline over 100 files:
// discovery results in, diagnostics out, cache disabled.
func BenchmarkCheckFiles(b *testing.B) {
	files := corpusFiles(b, 100)
	s := settings.Default()
	src := linter.Options{}
	prevDir, prevNoCache := G.CacheDir, G.NoCache
	G.NoCache = true
	b.Cleanup(func() { G.CacheDir, G.NoCache = prevDir, prevNoCache })

	b.ReportAllocs()
	b.SetBytes(int64(100 * len(benchFileSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := lintFiles(s, files, src, false); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckFilesCached measures the same pipeline with the content-
// addressed cache warm, which is the steady state for a watch loop.
func BenchmarkCheckFilesCached(b *testing.B) {
	files := corpusFiles(b, 100)
	s := settings.Default()
	src := linter.Options{}
	prevDir, prevNoCache := G.CacheDir, G.NoCache
	G.CacheDir = b.TempDir()
	G.NoCache = false
	b.Cleanup(func() { G.CacheDir, G.NoCache = prevDir, prevNoCache })

	if _, _, err := lintFiles(s, files, src, false); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(100 * len(benchFileSQL)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := lintFiles(s, files, src, false); err != nil {
			b.Fatal(err)
		}
	}
}
