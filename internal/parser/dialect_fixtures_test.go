package parser_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/source"
)

// dialectRoot is the fixture tree relative to this package.
const dialectRoot = "../../testdata/dialects"

// dialectCase is the optional <name>.toml next to a dialect fixture.
type dialectCase struct {
	// SyntaxErrors is the number of expected lexer/parser errors.
	SyntaxErrors int `toml:"syntax-errors"`
	// NoRoundTrip disables the byte-identical round-trip check, for inputs
	// that are intentionally not round-trippable.
	NoRoundTrip bool `toml:"no-round-trip"`
}

// TestDialectFixtures parses every fixture under testdata/dialects/<dialect>/
// with that dialect and asserts the parse is error-free and byte-exact.
func TestDialectFixtures(t *testing.T) {
	families, err := os.ReadDir(dialectRoot)
	if err != nil {
		t.Fatalf("read %s: %v", dialectRoot, err)
	}
	var total int
	for _, fam := range families {
		if !fam.IsDir() {
			continue
		}
		name := fam.Name()
		d, ok := dialect.Lookup(name)
		if !ok {
			t.Fatalf("%s: unknown dialect %q", dialectRoot, name)
		}
		dir := filepath.Join(dialectRoot, name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		var files []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
				files = append(files, e.Name())
			}
		}
		sort.Strings(files)
		for _, f := range files {
			f := f
			total++
			t.Run(name+"/"+strings.TrimSuffix(f, ".sql"), func(t *testing.T) {
				path := filepath.Join(dir, f)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				var c dialectCase
				if b, err := os.ReadFile(strings.TrimSuffix(path, ".sql") + ".toml"); err == nil {
					if err := toml.Unmarshal(b, &c); err != nil {
						t.Fatalf("parse %s.toml: %v", path, err)
					}
				}
				src := source.New(f, raw)
				toks, lexErrs := lexer.Lex(src, d)
				tr, parseErrs := parser.New().Parse(toks, d)
				got := len(lexErrs) + len(parseErrs)
				if got != c.SyntaxErrors {
					t.Fatalf("syntax errors = %d (lex %v, parse %v), want %d", got, lexErrs, parseErrs, c.SyntaxErrors)
				}
				if c.NoRoundTrip {
					return
				}
				if out := string(parser.Unparse(tr)); out != string(raw) {
					t.Fatalf("round-trip mismatch\n--- in ---\n%s\n--- out ---\n%s", raw, out)
				}
			})
		}
	}
	if total == 0 {
		t.Fatalf("no fixtures found under %s", dialectRoot)
	}
}
