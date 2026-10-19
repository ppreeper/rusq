package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppreeper/rusq/internal/config"
)

// BenchmarkConfigResolve measures hierarchical discovery over a four-deep tree,
// which is the per-directory cost the CLI amortises across a project.
func BenchmarkConfigResolve(b *testing.B) {
	// Hermetic: keep the user config out of the chain.
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	dir := b.TempDir()
	for _, name := range []string{"a", "b", "c", "d"} {
		dir = filepath.Join(dir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rusq.toml"), []byte("[lint]\nline-length = 100\n"), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := config.LoadChain(dir, "", false); err != nil {
			b.Fatal(err)
		}
	}
}
