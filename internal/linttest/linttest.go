// Package linttest is the shared harness for rule fixtures and rule tests.
//
// A rule fixture lives in testdata/rules/<CODE>/<case>/ and contains:
//
//	input.sql      the source under test (required)
//	case.toml      settings and expectations (optional; see Case)
//	expected.sql   the expected output of applying fixes (optional)
//
// The harness is deliberately explicit: a fixture that expects no findings
// says so with an empty diagnostics list, so a missing expectation can never
// be mistaken for a clean case.
package linttest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

// Options resolves the settings a fixture runs under.
type Options struct {
	Dialect     string
	Templater   string
	Select      []string
	Ignore      []string
	LineLength  int
	Rules       map[string]map[string]any
	UnsafeFixes bool
	// Index, when set, is handed to the linter so cross-file rules can run.
	Index *index.Index
}

// Settings returns the effective settings for the options.
func (o Options) Settings() settings.Settings {
	s := settings.Default()
	if o.Dialect != "" {
		if d, ok := dialect.Lookup(o.Dialect); ok {
			s.DialectName = d.Name
			s.Dialect = d
		}
	}
	if o.Templater != "" {
		s.Templater = o.Templater
	}
	if o.LineLength > 0 {
		s.LineLength = o.LineLength
		s.Format.LineLength = o.LineLength
	}
	if len(o.Select) > 0 {
		s.Set.Include = append([]string(nil), o.Select...)
	}
	if len(o.Ignore) > 0 {
		s.Set.Exclude = append([]string(nil), o.Ignore...)
	}
	if o.Rules != nil {
		s.Rules = o.Rules
	}
	return s
}

// Run lints sql under the options and returns the result.
func Run(t testing.TB, sql string, o Options) linter.Result {
	t.Helper()
	return RunBytes(t, "fixture.sql", []byte(sql), o)
}

// RunBytes lints src under the options and returns the result.
func RunBytes(t testing.TB, path string, src []byte, o Options) linter.Result {
	t.Helper()
	l := linter.New(o.Settings())
	return l.Run(source.New(path, src), linter.Options{Index: o.Index})
}

// Fix lints src and applies fixes, returning the result.
func Fix(t testing.TB, path string, src []byte, o Options) linter.Result {
	t.Helper()
	l := linter.New(o.Settings())
	return l.Run(source.New(path, src), linter.Options{Fix: true, UnsafeFixes: o.UnsafeFixes, Index: o.Index})
}

// Clean lints sql and asserts that it produces no diagnostics.
func Clean(t testing.TB, sql string, o Options) {
	t.Helper()
	res := Run(t, sql, o)
	AssertDiagnostics(t, []byte(sql), res.Diagnostics, nil)
	if len(res.SyntaxErrors) > 0 {
		t.Fatalf("unexpected syntax errors: %v", res.SyntaxErrors)
	}
}

// Want is one expected diagnostic: a 1-based position and a rule code.
type Want struct {
	Line int
	Col  int
	Code string
}

func (w Want) String() string { return fmt.Sprintf("%d:%d %s", w.Line, w.Col, w.Code) }

// ParseWant parses a "line:col CODE" expectation. The column may be omitted
// ("line CODE"); a zero column matches any column on that line.
func ParseWant(s string) (Want, error) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 2 {
		return Want{}, fmt.Errorf("bad expectation %q: want \"line:col CODE\"", s)
	}
	pos, code := fields[0], fields[1]
	var w Want
	w.Code = code
	if i := strings.IndexByte(pos, ':'); i >= 0 {
		line, err := strconv.Atoi(pos[:i])
		if err != nil {
			return Want{}, fmt.Errorf("bad expectation %q: %w", s, err)
		}
		col, err := strconv.Atoi(pos[i+1:])
		if err != nil {
			return Want{}, fmt.Errorf("bad expectation %q: %w", s, err)
		}
		w.Line, w.Col = line, col
	} else {
		line, err := strconv.Atoi(pos)
		if err != nil {
			return Want{}, fmt.Errorf("bad expectation %q: %w", s, err)
		}
		w.Line = line
	}
	if w.Line < 1 || w.Col < 0 {
		return Want{}, fmt.Errorf("bad expectation %q: positions are 1-based", s)
	}
	return w, nil
}

// Actual renders the diagnostics of a run in the same "line:col CODE" form as
// an expectation.
func Actual(src []byte, ds []diagnostic.Diagnostic) []Want {
	loc := source.NewLocator(src)
	out := make([]Want, 0, len(ds))
	for _, d := range ds {
		p := loc.Position(d.Span.Start)
		out = append(out, Want{Line: p.Line, Col: p.Col, Code: d.Code})
	}
	return out
}

// AssertDiagnostics asserts that got matches want exactly (same multiset of
// code and position). A zero column in an expectation matches any column.
func AssertDiagnostics(t testing.TB, src []byte, got []diagnostic.Diagnostic, want []string) {
	t.Helper()
	wants := make([]Want, 0, len(want))
	for _, s := range want {
		w, err := ParseWant(s)
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
		wants = append(wants, w)
	}
	actual := Actual(src, got)

	matched := make([]bool, len(wants))
	var missing []Want
	for _, a := range actual {
		ok := false
		for i, w := range wants {
			if matched[i] {
				continue
			}
			if w.Code != a.Code || w.Line != a.Line {
				continue
			}
			if w.Col != 0 && w.Col != a.Col {
				continue
			}
			matched[i] = true
			ok = true
			break
		}
		if !ok {
			missing = append(missing, a)
		}
	}
	var unmatched []Want
	for i, w := range wants {
		if !matched[i] {
			unmatched = append(unmatched, w)
		}
	}
	if len(missing) == 0 && len(unmatched) == 0 {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "diagnostics mismatch\n  want: %v\n  got:  %v", formatWants(wants), formatWants(actual))
	if len(unmatched) > 0 {
		fmt.Fprintf(&b, "\n  expected but not reported: %v", formatWants(unmatched))
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\n  reported but not expected: %v", formatWants(missing))
	}
	if len(got) > 0 {
		b.WriteString("\n  detail:")
		for _, d := range got {
			p := source.NewLocator(src).Position(d.Span.Start)
			fmt.Fprintf(&b, "\n    %d:%d %s %s: %s", p.Line, p.Col, d.Code, d.Name, d.Message)
			if d.Fixability != diagnostic.NoFix {
				fmt.Fprintf(&b, " [fix: %s]", d.Fixability)
			}
		}
	}
	t.Fatal(b.String())
}

// AssertFix asserts that applying fixes to sql yields want, and that the
// result is stable under a second fix pass.
func AssertFix(t testing.TB, sql string, o Options, want string) {
	t.Helper()
	res := Fix(t, "fixture.sql", []byte(sql), o)
	if len(res.SyntaxErrors) > 0 {
		t.Fatalf("unexpected syntax errors: %v", res.SyntaxErrors)
	}
	got := string(res.FixedSource)
	if got != want {
		t.Fatalf("fix output mismatch\n--- want ---\n%s\n--- got ---\n%s", visible(want), visible(got))
	}
	again := Fix(t, "fixture.sql", res.FixedSource, o)
	if again.Changed {
		t.Fatalf("fix is not idempotent; second pass still changes the file:\n%s", visible(string(again.FixedSource)))
	}
}

// AssertNoSyntaxErrors fails if the result carries lexer/parser errors.
func AssertNoSyntaxErrors(t testing.TB, res linter.Result) {
	t.Helper()
	if len(res.SyntaxErrors) > 0 {
		t.Fatalf("unexpected syntax errors: %v", res.SyntaxErrors)
	}
}

func formatWants(ws []Want) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.String()
	}
	sort.Strings(out)
	return out
}

// visible makes trailing whitespace visible in diffs.
func visible(s string) string {
	return strings.ReplaceAll(s, " \n", "·\n")
}
