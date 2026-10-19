package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEntry(t *testing.T, c *Cache, key string, size int, age time.Duration) string {
	t.Helper()
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPruneEvictsOldestFirst(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	old := writeEntry(t, c, "aa"+repeat("1", 62), 1000, time.Hour)
	mid := writeEntry(t, c, "bb"+repeat("2", 62), 1000, 30*time.Minute)
	fresh := writeEntry(t, c, "cc"+repeat("3", 62), 1000, time.Minute)

	removed, err := c.Prune(2000)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("oldest entry should have been evicted")
	}
	for _, p := range []string{mid, fresh} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("entry %s should have been kept: %v", p, err)
		}
	}
}

func TestPruneRemovesEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	p := writeEntry(t, c, "dd"+repeat("4", 62), 500, time.Hour)
	sub := filepath.Dir(p)
	if _, err := c.Prune(1); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Errorf("empty shard directory should have been removed")
	}
}

func TestPruneNoCapIsNoOp(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	p := writeEntry(t, c, "ee"+repeat("5", 62), 100, time.Hour)
	for _, max := range []int64{0, -1} {
		removed, err := c.Prune(max)
		if err != nil {
			t.Fatalf("Prune(%d): %v", max, err)
		}
		if removed != 0 {
			t.Errorf("Prune(%d) removed %d, want 0", max, removed)
		}
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("entry should have been kept: %v", err)
	}
}

func TestPruneUnderBudgetIsNoOp(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	p := writeEntry(t, c, "ff"+repeat("6", 62), 100, time.Hour)
	removed, err := c.Prune(1 << 20)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("entry should have been kept: %v", err)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
