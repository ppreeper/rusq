// Package formatest is the shared harness for formatter goldens.
//
// A format fixture lives in testdata/format/<case>/ and contains:
//
//	input.sql      the source to format (required)
//	expected.sql   the expected formatted output (required)
//	case.toml      settings overrides (optional; see Settings)
//
// Every fixture is additionally checked for idempotency and for preservation
// of the significant token stream, because a formatter that changes either is
// broken by definition.
package formatest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/ppreeper/rusq/internal/formatter"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

// Settings overrides the formatter settings for a fixture.
type Settings struct {
	Dialect             string                  `toml:"dialect"`
	LineLength          int                     `toml:"line-length"`
	IndentUnit          string                  `toml:"indent-unit"`
	IndentWidth         int                     `toml:"indent-width"`
	CommaStyle          string                  `toml:"comma-style"`
	LinePosition        string                  `toml:"line-position"`
	KeywordLinePosition string                  `toml:"keyword-line-position"`
	SingleTargetPolicy  string                  `toml:"single-target-policy"`
	AlignFollowing      bool                    `toml:"align-following"`
	CteBracketNewline   bool                    `toml:"cte-bracket-newline"`
	MaxBlankLines       int                     `toml:"max-blank-lines"`
	Type                map[string]TypeSettings `toml:"type"`
}

// TypeSettings overrides layout for one segment type.
type TypeSettings struct {
	LinePosition string `toml:"line-position"`
}

// Settings resolves the effective settings.
func (c Settings) Settings() settings.Settings {
	s := settings.Default()
	if c.LineLength > 0 {
		s.Format.LineLength = c.LineLength
		s.LineLength = c.LineLength
	}
	if c.IndentUnit != "" {
		s.Format.IndentUnit = c.IndentUnit
	}
	if c.IndentWidth > 0 {
		s.Format.IndentWidth = c.IndentWidth
	}
	if c.CommaStyle != "" {
		s.Format.CommaStyle = c.CommaStyle
	}
	if c.LinePosition != "" {
		s.Format.LinePosition = c.LinePosition
	}
	if c.KeywordLinePosition != "" {
		s.Format.KeywordLinePosition = c.KeywordLinePosition
	}
	if c.SingleTargetPolicy != "" {
		s.Format.SingleTargetPolicy = c.SingleTargetPolicy
	}
	if c.AlignFollowing {
		s.Format.AlignFollowing = true
	}
	if c.CteBracketNewline {
		s.Format.CteBracketNewline = true
	}
	if c.MaxBlankLines > 0 {
		s.Format.MaxBlankLines = c.MaxBlankLines
	}
	if len(c.Type) > 0 {
		s.Format.Types = make(map[string]settings.TypeFormat, len(c.Type))
		for k, v := range c.Type {
			s.Format.Types[k] = settings.TypeFormat{LinePosition: v.LinePosition}
		}
	}
	return s
}

// Format formats sql under the settings.
func Format(sql string, s settings.Settings) string {
	return string(formatter.New(s).FormatBytes([]byte(sql)))
}

// AssertFormat asserts that formatting sql yields want.
func AssertFormat(t testing.TB, sql, want string, s settings.Settings) {
	t.Helper()
	got := Format(sql, s)
	if got != want {
		t.Fatalf("format mismatch\n--- want ---\n%s\n--- got ---\n%s", visible(want), visible(got))
	}
	// Idempotency: formatting the output must be a no-op.
	again := Format(got, s)
	if again != got {
		t.Fatalf("format is not idempotent\n--- first ---\n%s\n--- second ---\n%s", visible(got), visible(again))
	}
	// Token preservation: formatting must not add, drop, or reorder
	// significant tokens.
	if a, b := tokens(sql, s), tokens(got, s); a != b {
		t.Fatalf("format changed the significant token stream\n  in:  %s\n  out: %s", a, b)
	}
}

// Case is one discovered format fixture.
type Case struct {
	Name     string
	Dir      string
	Input    string
	Expected string
	Settings Settings
}

// Discover finds every fixture under root (normally testdata/format).
func Discover(t testing.TB, root string) []Case {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	var out []Case
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		in, err := os.ReadFile(filepath.Join(dir, "input.sql"))
		if err != nil {
			t.Fatalf("%s: missing input.sql", dir)
		}
		c := Case{Name: e.Name(), Dir: dir, Input: string(in)}
		if b, err := os.ReadFile(filepath.Join(dir, "expected.sql")); err == nil {
			c.Expected = string(b)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "case.toml")); err == nil {
			if err := toml.Unmarshal(b, &c.Settings); err != nil {
				t.Fatalf("%s: parse case.toml: %v", dir, err)
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RunCase runs one fixture, writing goldens when update is true.
func RunCase(t testing.TB, c Case, update bool) {
	t.Helper()
	s := c.Settings.Settings()
	got := Format(c.Input, s)
	if update {
		if err := os.WriteFile(filepath.Join(c.Dir, "expected.sql"), []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	if c.Expected == "" {
		t.Fatalf("no expected.sql golden; run with -update to create one")
	}
	AssertFormat(t, c.Input, c.Expected, s)
}

// tokens renders the significant token stream (kind and text) for comparison.
func tokens(sql string, s settings.Settings) string {
	src := source.New("t.sql", []byte(sql))
	toks, _ := lexer.Lex(src, s.Dialect)
	var b strings.Builder
	for _, t := range toks {
		switch t.Kind {
		case lexer.Whitespace, lexer.Newline, lexer.EOF:
			continue
		}
		b.WriteString(t.Kind.String())
		b.WriteByte(':')
		b.WriteString(t.Text)
		b.WriteByte(' ')
	}
	return b.String()
}

// visible makes trailing whitespace visible in diffs.
func visible(s string) string {
	return strings.ReplaceAll(s, " \n", "·\n")
}
