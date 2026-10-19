package cache_test

import (
	"testing"

	"github.com/ppreeper/rusq/internal/cache"
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/settings"
)

// BenchmarkCacheHit measures the warm path: a stat, a read, a decode.
func BenchmarkCacheHit(b *testing.B) {
	c := cache.New(b.TempDir())
	key := cache.Key("a.sql", []byte("SELECT 1;\n"), cache.SettingsHash(settings.Default()))
	ds := []diagnostic.Diagnostic{
		{Code: "CV05", Name: "convention.is_null", Message: "Use IS when comparing with NULL."},
	}
	c.Put(key, ds)
	if _, ok := c.Get(key); !ok {
		b.Fatal("warm-up miss")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := c.Get(key); !ok {
			b.Fatal("miss")
		}
	}
}
