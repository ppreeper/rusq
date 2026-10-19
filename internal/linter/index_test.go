package linter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/walk"
)

// crossFileProject writes two files that define the same table, and a file whose
// CTE is only referenced from another file.
func crossFileProject(t *testing.T) (string, []walk.File) {
	t.Helper()
	dir := t.TempDir()
	bodies := map[string]string{
		"a.sql": "CREATE TABLE users (id INT);\n",
		"b.sql": "CREATE TABLE users (id INT);\n",
		"c.sql": "WITH helper AS (SELECT 1 AS x)\nSELECT 1 AS y;\n",
		"d.sql": "SELECT * FROM helper;\n",
	}
	var files []walk.File
	for name, body := range bodies {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, walk.File{Path: p})
	}
	return dir, files
}

func codes(ds []diagnostic.Diagnostic) map[string]int {
	out := map[string]int{}
	for _, d := range ds {
		out[d.Code]++
	}
	return out
}

func TestCrossFileUnusedCTEIsSuppressed(t *testing.T) {
	dir, files := crossFileProject(t)
	s := settings.Default()
	ix, err := index.Build(files, index.Options{Settings: s, NoCache: true})
	if err != nil {
		t.Fatal(err)
	}

	c := filepath.Join(dir, "c.sql")
	b, err := os.ReadFile(c)
	if err != nil {
		t.Fatal(err)
	}
	src := source.New(c, b)

	// Without an index the CTE looks unused.
	local := New(s).Run(src, Options{})
	if codes(local.Diagnostics)["ST03"] != 1 {
		t.Fatalf("want ST03 without an index, got %v", codes(local.Diagnostics))
	}

	// With the project index, d.sql references it, so no finding.
	withIndex := New(s).Run(src, Options{Index: ix})
	if got := codes(withIndex.Diagnostics)["ST03"]; got != 0 {
		t.Fatalf("want no ST03 with the index, got %d", got)
	}
}

func TestCrossFileDuplicateDefinition(t *testing.T) {
	dir, files := crossFileProject(t)
	s := settings.Default()
	ix, err := index.Build(files, index.Options{Settings: s, NoCache: true})
	if err != nil {
		t.Fatal(err)
	}

	read := func(name string) source.Text {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return source.New(p, b)
	}

	// b.sql sorts after a.sql, so the duplicate is reported there.
	res := New(s).Run(read("b.sql"), Options{Index: ix})
	if got := codes(res.Diagnostics)["ST13"]; got != 1 {
		t.Fatalf("want one ST13 on b.sql, got %d (%v)", got, res.Diagnostics)
	}

	// The canonical definition is not reported.
	res = New(s).Run(read("a.sql"), Options{Index: ix})
	if got := codes(res.Diagnostics)["ST13"]; got != 0 {
		t.Fatalf("want no ST13 on a.sql, got %d", got)
	}

	// Without an index, cross-file rules stay silent.
	res = New(s).Run(read("b.sql"), Options{})
	if got := codes(res.Diagnostics)["ST13"]; got != 0 {
		t.Fatalf("want no ST13 without an index, got %d", got)
	}
}
