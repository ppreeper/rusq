package walk_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppreeper/rusq/internal/walk"
)

// BenchmarkWalk measures discovery over a 200-file tree.
func BenchmarkWalk(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 200; i++ {
		p := filepath.Join(dir, fmt.Sprintf("f%03d.sql", i))
		if err := os.WriteFile(p, []byte("SELECT 1;\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	opts := walk.Options{Roots: []string{dir}, Exts: []string{".sql"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := walk.Discover(opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Files) != 200 {
			b.Fatalf("found %d files, want 200", len(res.Files))
		}
	}
}
