package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppreeper/rusq/internal/config"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverClosestWins(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	write(t, filepath.Join(root, "rusq.toml"), "dialect = \"postgres\"\nline-length = 100\n")
	write(t, filepath.Join(root, "a", "rusq.toml"), "line-length = 80\n")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c, files, err := config.LoadChain(sub, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Dialect != "postgres" {
		t.Errorf("dialect = %q", c.Dialect)
	}
	if c.LineLength != 80 {
		t.Errorf("line-length = %d, want 80", c.LineLength)
	}
	if len(files) < 2 {
		t.Errorf("expected at least 2 config files, got %v", files)
	}
}

func TestExtend(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "base.toml"), "dialect = \"snowflake\"\ntemplater = \"jinja\"\n")
	write(t, filepath.Join(root, "rusq.toml"), "extend = \"base.toml\"\nline-length = 90\n")
	c, _, err := config.LoadChain(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Dialect != "snowflake" || c.Templater != "jinja" || c.LineLength != 90 {
		t.Errorf("merged config = %+v", c)
	}
}

func TestIsolated(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "rusq.toml"), "dialect = \"mysql\"\n")
	c, files, err := config.LoadChain(root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Dialect != "" || len(files) != 0 {
		t.Errorf("isolated config = %+v files=%v", c, files)
	}
}
