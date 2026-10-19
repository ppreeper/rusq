package linttest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/rules"
)

// Case is the optional case.toml next to a fixture.
type Case struct {
	Settings CaseSettings              `toml:"settings"`
	Rules    map[string]map[string]any `toml:"rules"`
	Expect   CaseExpect                `toml:"expect"`
}

// CaseSettings overrides the run settings for a fixture.
type CaseSettings struct {
	Dialect    string   `toml:"dialect"`
	Templater  string   `toml:"templater"`
	Select     []string `toml:"select"`
	Ignore     []string `toml:"ignore"`
	LineLength int      `toml:"line-length"`
	Unsafe     bool     `toml:"unsafe"`
}

// CaseExpect states what a fixture must produce.
type CaseExpect struct {
	// Diagnostics is the exact set of expected findings, each "line:col CODE"
	// (or "line CODE" when the column is not significant).
	Diagnostics []string `toml:"diagnostics"`
	// SyntaxErrors is the number of expected lexer/parser errors.
	SyntaxErrors int `toml:"syntax-errors"`
	// Fix marks a fixture as a fix case; expected.sql must exist.
	Fix bool `toml:"fix"`
}

// Options converts the case to harness options.
func (c Case) Options(fallbackSelect string) Options {
	sel := c.Settings.Select
	if len(sel) == 0 && fallbackSelect != "" {
		sel = []string{fallbackSelect}
	}
	return Options{
		Dialect:     c.Settings.Dialect,
		Templater:   c.Settings.Templater,
		Select:      sel,
		Ignore:      c.Settings.Ignore,
		LineLength:  c.Settings.LineLength,
		Rules:       c.Rules,
		UnsafeFixes: c.Settings.Unsafe,
	}
}

// RuleCase is one discovered rule fixture.
type RuleCase struct {
	Code     string
	Name     string
	Dir      string
	Input    []byte
	Expected []byte // nil when there is no expected.sql
	Case     Case
	// ProjectDir, when set, holds extra .sql files built into a project index
	// (testdata/rules/<CODE>/<case>/project/) for cross-file rules.
	ProjectDir string
}

// DiscoverRules finds every fixture under root (normally testdata/rules).
func DiscoverRules(t testing.TB, root string) []RuleCase {
	t.Helper()
	known := map[string]bool{}
	for _, c := range rules.Codes() {
		known[c] = true
	}
	var out []RuleCase
	families, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	for _, fam := range families {
		if !fam.IsDir() {
			continue
		}
		code := fam.Name()
		if !known[code] {
			t.Fatalf("%s: %q is not a registered rule code", root, code)
		}
		codeDir := filepath.Join(root, code)
		names, err := os.ReadDir(codeDir)
		if err != nil {
			t.Fatalf("read %s: %v", codeDir, err)
		}
		for _, n := range names {
			if !n.IsDir() {
				continue
			}
			dir := filepath.Join(codeDir, n.Name())
			input, err := os.ReadFile(filepath.Join(dir, "input.sql"))
			if err != nil {
				t.Fatalf("%s: missing input.sql", dir)
			}
			c := RuleCase{Code: code, Dir: dir, Input: input}
			c.Name = filepath.ToSlash(filepath.Join(code, n.Name()))
			if st, err := os.Stat(filepath.Join(dir, "project")); err == nil && st.IsDir() {
				c.ProjectDir = filepath.Join(dir, "project")
			}
			if b, err := os.ReadFile(filepath.Join(dir, "expected.sql")); err == nil {
				c.Expected = b
			}
			if b, err := os.ReadFile(filepath.Join(dir, "case.toml")); err == nil {
				if err := toml.Unmarshal(b, &c.Case); err != nil {
					t.Fatalf("%s: parse case.toml: %v", dir, err)
				}
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// projectIndex builds an in-memory project index: the fixture input under the
// path "input.sql" plus every .sql file directly inside the fixture's project/
// directory under its own base name.
func projectIndex(t testing.TB, c RuleCase, opts Options) *index.Index {
	t.Helper()
	d := opts.Settings().Dialect
	ix := index.New()
	ix.Put("input.sql", c.Input, d)
	entries, err := os.ReadDir(c.ProjectDir)
	if err != nil {
		t.Fatalf("%s: read project dir: %v", c.Dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.ProjectDir, e.Name()))
		if err != nil {
			t.Fatalf("%s: read %s: %v", c.Dir, e.Name(), err)
		}
		ix.Put(e.Name(), b, d)
	}
	return ix
}

// RunRuleCase runs one fixture, writing goldens when update is true.
func RunRuleCase(t testing.TB, c RuleCase, update bool) {
	t.Helper()
	opts := c.Case.Options(c.Code)
	src := c.Input
	if c.ProjectDir != "" {
		opts.Index = projectIndex(t, c, opts)
	}

	res := RunBytes(t, "input.sql", src, opts)
	if got, want := len(res.SyntaxErrors), c.Case.Expect.SyntaxErrors; got != want {
		t.Fatalf("syntax errors = %d (%v), want %d", got, res.SyntaxErrors, want)
	}
	AssertDiagnostics(t, src, res.Diagnostics, c.Case.Expect.Diagnostics)

	if c.Expected == nil && !c.Case.Expect.Fix {
		return
	}

	fixed := Fix(t, "input.sql", src, opts)
	if len(fixed.SyntaxErrors) > 0 {
		t.Fatalf("unexpected syntax errors while fixing: %v", fixed.SyntaxErrors)
	}
	if update {
		if err := os.WriteFile(filepath.Join(c.Dir, "expected.sql"), fixed.FixedSource, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	if c.Expected == nil {
		t.Fatalf("no expected.sql golden; run with -update to create one")
	}
	if got, want := string(fixed.FixedSource), string(c.Expected); got != want {
		t.Fatalf("fix output mismatch\n--- want ---\n%s\n--- got ---\n%s", visible(want), visible(got))
	}
	again := Fix(t, "input.sql", fixed.FixedSource, opts)
	if again.Changed {
		t.Fatalf("fix is not idempotent; second pass still changes the file:\n%s", visible(string(again.FixedSource)))
	}
}
