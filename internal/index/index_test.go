package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/walk"
)

// project writes a small multi-file project and returns its directory.
func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a.sql": "CREATE TABLE users (id INT);\n",
		"b.sql": "CREATE TABLE users (id INT);\n",
		"c.sql": "WITH helper AS (SELECT 1 AS x)\nSELECT 1 AS y;\n",
		"d.sql": "SELECT * FROM helper;\n",
		"e.sql": "CREATE VIEW sales.v AS SELECT * FROM orders;\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func build(t *testing.T, dir string, noCache bool) *Index {
	t.Helper()
	var files []walk.File
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		files = append(files, walk.File{Path: filepath.Join(dir, e.Name())})
	}
	ix, err := Build(files, Options{
		Settings: settings.Default(),
		CacheDir: filepath.Join(dir, ".rusq_cache"),
		NoCache:  noCache,
		Workers:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestBuildFacts(t *testing.T) {
	dir := project(t)
	ix := build(t, dir, true)
	if ix.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", ix.Len())
	}

	a := filepath.Join(dir, "a.sql")
	defs := ix.DefinitionsInPath(a)
	if len(defs) != 1 || defs[0].Kind != "table" || defs[0].Name != "users" {
		t.Fatalf("a.sql defs = %+v", defs)
	}

	c := filepath.Join(dir, "c.sql")
	cdefs := ix.DefinitionsInPath(c)
	if len(cdefs) != 1 || cdefs[0].Kind != "cte" || cdefs[0].Name != "helper" {
		t.Fatalf("c.sql defs = %+v", cdefs)
	}

	// Qualified definitions keep their qualification but also match on base.
	e := filepath.Join(dir, "e.sql")
	edefs := ix.DefinitionsInPath(e)
	if len(edefs) != 1 || edefs[0].Kind != "view" || edefs[0].Name != "sales.v" {
		t.Fatalf("e.sql defs = %+v", edefs)
	}
	if got := ix.DefinitionFiles("v"); len(got) != 1 || got[0] != e {
		t.Fatalf("DefinitionFiles(v) = %v", got)
	}
	if got := ix.DefinitionFiles("orders"); len(got) != 0 {
		t.Fatalf("DefinitionFiles(orders) = %v, want none (references are not definitions)", got)
	}
	if !ix.IsReferenced("orders") {
		t.Fatal("orders should be referenced by e.sql")
	}
}

func TestDuplicatesAndCrossFileReferences(t *testing.T) {
	dir := project(t)
	ix := build(t, dir, true)

	users := ix.DefinitionFiles("users")
	if len(users) != 2 {
		t.Fatalf("DefinitionFiles(users) = %v, want 2 files", users)
	}
	if users[0] != filepath.Join(dir, "a.sql") || users[1] != filepath.Join(dir, "b.sql") {
		t.Fatalf("duplicate order = %v, want a.sql then b.sql", users)
	}
	if got := ix.DefinitionFiles("nothing"); len(got) != 0 {
		t.Fatalf("DefinitionFiles(nothing) = %v", got)
	}

	c := filepath.Join(dir, "c.sql")
	if !ix.ReferencedElsewhere(c, "helper") {
		t.Fatal("helper is referenced by d.sql")
	}
	if ix.ReferencedElsewhere(c, "users") {
		t.Fatal("users is not referenced by any file")
	}
}

func TestBuildReusesAndInvalidatesCache(t *testing.T) {
	dir := project(t)
	cacheDir := filepath.Join(dir, ".rusq_cache")

	ix1 := build(t, dir, false)
	if _, err := os.Stat(filepath.Join(cacheDir, FileName)); err != nil {
		t.Fatalf("index store not written: %v", err)
	}

	// A second build with identical content reuses the stored facts.
	ix2 := build(t, dir, false)
	if got, want := len(ix2.DefinitionsInPath(filepath.Join(dir, "a.sql"))), 1; got != want {
		t.Fatalf("cached build defs = %d, want %d", got, want)
	}
	if ix2.DefinitionFiles("users")[0] != ix1.DefinitionFiles("users")[0] {
		t.Fatal("cached build lost duplicate information")
	}

	// Changing a file invalidates just that file's facts.
	b := filepath.Join(dir, "b.sql")
	if err := os.WriteFile(b, []byte("CREATE TABLE accounts (id INT);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix3 := build(t, dir, false)
	if got := ix3.DefinitionFiles("users"); len(got) != 1 {
		t.Fatalf("after edit, DefinitionFiles(users) = %v, want 1", got)
	}
	if got := ix3.DefinitionFiles("accounts"); len(got) != 1 {
		t.Fatalf("after edit, DefinitionFiles(accounts) = %v, want 1", got)
	}
}

func TestNilIndexIsSafe(t *testing.T) {
	var ix *Index
	if ix.Len() != 0 || ix.IsReferenced("x") || ix.ReferencedElsewhere("p", "x") {
		t.Fatal("nil index should be inert")
	}
	if ix.DefinitionsOf("x") != nil || ix.Paths() != nil || ix.DefinitionsInPath("p") != nil {
		t.Fatal("nil index queries should return nil")
	}
}

func TestBuildToleratesUnparseableAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.sql")
	if err := os.WriteFile(bad, []byte("SELECT ((((;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := Build([]walk.File{
		{Path: bad},
		{Path: filepath.Join(dir, "missing.sql")},
	}, Options{Settings: settings.Default(), NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if ix.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (missing file skipped)", ix.Len())
	}
}

func TestBase(t *testing.T) {
	cases := map[string]string{
		"users":    "users",
		"sales.v":  "v",
		"a.b.c":    "c",
		`"quoted"`: `"quoted"`,
		"":         "",
	}
	for in, want := range cases {
		if got := Base(in); got != want {
			t.Errorf("Base(%q) = %q, want %q", in, got, want)
		}
	}
	if d := (Definition{Name: "S.T"}); d.Base() != "t" && d.Base() != "T" {
		t.Errorf("Definition.Base() = %q", d.Base())
	}
}

func TestDialectIsPartOfTheCacheKey(t *testing.T) {
	dir := project(t)
	cacheDir := filepath.Join(dir, ".rusq_cache")
	files := []walk.File{{Path: filepath.Join(dir, "a.sql")}}
	if _, err := Build(files, Options{Settings: settings.Default(), CacheDir: cacheDir}); err != nil {
		t.Fatal(err)
	}
	other := settings.Default()
	d, ok := dialect.Lookup("postgres")
	if !ok {
		t.Fatal("postgres dialect not found")
	}
	other.Dialect = d
	other.DialectName = "postgres"
	ix, err := Build(files, Options{Settings: other, CacheDir: cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	if ix.Len() != 1 {
		t.Fatalf("Len() = %d", ix.Len())
	}
}
