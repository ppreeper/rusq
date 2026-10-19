// Package index builds the opt-in cross-file project index.
//
// The index records, per file, the objects the file *defines* (tables, views
// and CTEs) and the objects it *references* (tables named in FROM/JOIN clauses).
// It is deliberately cheap: each file is lexed, parsed and projected once, and
// the resulting facts are small, so a whole project fits in memory and can be
// cached on disk between runs.
//
// Cross-file rules consume the index through the lint context; see
// docs/RULES.md §13 and docs/SPEC.md §8.3.
package index

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
	"github.com/ppreeper/rusq/internal/walk"
	"golang.org/x/crypto/blake2b"
)

func hashBytes(b []byte) string {
	sum := blake2b.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Version stamps the on-disk index format. A mismatch discards the cache.
const Version = "1"

// FileName is the index file inside the cache directory.
const FileName = "index.json"

// Definition is an object defined by a file.
type Definition struct {
	Kind  string `json:"kind"` // "table", "view" or "cte"
	Name  string `json:"name"` // lower-cased, possibly schema-qualified
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Base returns the unqualified part of the definition name.
func (d Definition) Base() string { return Base(d.Name) }

// Base returns the last dot-separated segment of a possibly qualified name.
func Base(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Reference is a use of an object.
type Reference struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// facts are the index records for one file.
type facts struct {
	Hash string       `json:"hash"`
	Defs []Definition `json:"defs,omitempty"`
	Refs []Reference  `json:"refs,omitempty"`
}

// store is the on-disk index.
type store struct {
	Version  string            `json:"version"`
	Settings string            `json:"settings,omitempty"`
	Files    map[string]*facts `json:"files"`
}

// Index is the resolved project index.
type Index struct {
	files map[string]*facts
}

// New returns an empty index.
func New() *Index { return &Index{files: map[string]*facts{}} }

// Put records the facts for one file from in-memory source, replacing any facts
// already recorded for it. Incremental consumers (the language server) use it so
// that an unsaved buffer participates in cross-file analysis.
func (ix *Index) Put(path string, src []byte, d *dialect.Dialect) {
	if ix == nil {
		return
	}
	key := filepath.ToSlash(path)
	if ix.files == nil {
		ix.files = map[string]*facts{}
	}
	ix.files[key] = extract(key, src, d)
}

// Remove drops one file from the index.
func (ix *Index) Remove(path string) {
	if ix == nil {
		return
	}
	delete(ix.files, filepath.ToSlash(path))
}

// DefinitionCount returns how many definitions the index holds in total.
func (ix *Index) DefinitionCount() int {
	if ix == nil {
		return 0
	}
	n := 0
	for _, f := range ix.files {
		n += len(f.Defs)
	}
	return n
}

// Options controls a build.
type Options struct {
	Settings settings.Settings
	CacheDir string
	NoCache  bool
	Workers  int
}

// Build reads and indexes every file.
func Build(files []walk.File, opts Options) (*Index, error) {
	ix := New()
	if len(files) == 0 {
		return ix, nil
	}
	st := loadStore(opts)

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers > len(files) {
		workers = len(files)
	}

	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, workers)
	)
	out := make([]*facts, len(files))
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f walk.File) {
			defer wg.Done()
			defer func() { <-sem }()
			b, err := os.ReadFile(f.Path)
			if err != nil {
				return
			}
			key := filepath.ToSlash(f.Path)
			h := hashBytes(b)
			if st != nil {
				if prev, ok := st.Files[key]; ok && prev.Hash == h {
					out[i] = prev
					return
				}
			}
			out[i] = extract(key, b, opts.Settings.Dialect)
		}(i, f)
	}
	wg.Wait()

	next := &store{Version: Version, Settings: opts.Settings.DialectName, Files: map[string]*facts{}}
	for i, f := range files {
		if out[i] == nil {
			continue
		}
		key := filepath.ToSlash(f.Path)
		ix.files[key] = out[i]
		next.Files[key] = out[i]
	}
	saveStore(opts, next)
	return ix, nil
}

// extract computes the facts for one file's bytes.
func extract(path string, b []byte, d *dialect.Dialect) *facts {
	f := &facts{Hash: hashBytes(b)}
	txt := source.New(path, b)
	tokens, _ := lexer.Lex(txt, d)
	tr, _ := parser.New().Parse(tokens, d)
	if tr == nil {
		return f
	}
	file := ast.Project(tr)
	for _, st := range file.Statements {
		if kind, name, span, ok := ddlDefinition(st.Node); ok {
			f.Defs = append(f.Defs, Definition{
				Kind:  kind,
				Name:  strings.ToLower(name),
				Start: span.Start,
				End:   span.End,
			})
		}
		if st.Select == nil {
			continue
		}
		if st.Select.With != nil {
			for _, cte := range st.Select.With.Ctes {
				span := cteSpan(cte)
				f.Defs = append(f.Defs, Definition{
					Kind:  "cte",
					Name:  strings.ToLower(cte.Name),
					Start: span.Start,
					End:   span.End,
				})
			}
		}
		for _, ref := range allTables(st.Select) {
			if ref.Name == "" {
				continue
			}
			span := tableSpan(ref)
			f.Refs = append(f.Refs, Reference{
				Name:  strings.ToLower(ref.Name),
				Start: span.Start,
				End:   span.End,
			})
		}
	}
	return f
}

func allTables(s *ast.Select) []*ast.TableRef {
	out := append([]*ast.TableRef{}, s.Tables...)
	for _, j := range s.Joins {
		if j.Table != nil {
			out = append(out, j.Table)
		}
	}
	return out
}

func cteSpan(cte *ast.Cte) source.Range {
	if cte.Node != nil {
		return cte.Node.Span()
	}
	return source.Range{}
}

func tableSpan(tr *ast.TableRef) source.Range {
	if len(tr.Tokens) > 0 {
		return source.Range{Start: tr.Tokens[0].Span.Start, End: tr.Tokens[len(tr.Tokens)-1].Span.End}
	}
	return source.Range{}
}

// ddlDefinition recognises CREATE TABLE / CREATE VIEW statements and returns the
// kind, the (possibly qualified) name and the name's span.
func ddlDefinition(n *tree.Node) (kind, name string, span source.Range, ok bool) {
	if n == nil {
		return "", "", span, false
	}
	sig := significant(n.Leaves(nil))
	if len(sig) < 3 || sig[0].Upper() != "CREATE" {
		return "", "", span, false
	}
	i := 1
	for i < len(sig) && ddlModifier(sig[i].Upper()) {
		i++
	}
	if i >= len(sig) {
		return "", "", span, false
	}
	switch sig[i].Upper() {
	case "TABLE":
		kind = "table"
	case "VIEW":
		kind = "view"
	case "MATERIALIZED":
		if i+1 >= len(sig) || sig[i+1].Upper() != "VIEW" {
			return "", "", span, false
		}
		kind = "view"
		i++
	default:
		return "", "", span, false
	}
	i++
	if i+2 < len(sig) && sig[i].Upper() == "IF" && sig[i+1].Upper() == "NOT" && sig[i+2].Upper() == "EXISTS" {
		i += 3
	}
	if i >= len(sig) || !isName(sig[i]) {
		return "", "", span, false
	}
	start := i
	var parts []string
	for i < len(sig) {
		if isName(sig[i]) {
			parts = append(parts, sig[i].Text)
			i++
			if i < len(sig) && sig[i].Kind == lexer.Dot {
				i++
				continue
			}
		}
		break
	}
	if len(parts) == 0 {
		return "", "", span, false
	}
	last := i - 1
	if last < start {
		last = start
	}
	return kind, strings.Join(parts, "."), source.Range{Start: sig[start].Span.Start, End: sig[last].Span.End}, true
}

func ddlModifier(up string) bool {
	switch up {
	case "OR", "REPLACE", "TEMP", "TEMPORARY", "GLOBAL", "LOCAL", "EXTERNAL",
		"UNLOGGED", "TRANSIENT", "UNIQUE", "CLUSTERED", "NONCLUSTERED", "VIRTUAL":
		return true
	}
	return false
}

func isName(t *lexer.Token) bool {
	return t.Kind == lexer.Identifier || t.Kind == lexer.QuotedIdentifier || t.Kind == lexer.Keyword
}

func significant(toks []*lexer.Token) []*lexer.Token {
	out := make([]*lexer.Token, 0, len(toks))
	for _, t := range toks {
		if t.IsSignificant() {
			out = append(out, t)
		}
	}
	return out
}

// --- queries ---------------------------------------------------------------

// LocatedDefinition is a definition together with the file that holds it.
type LocatedDefinition struct {
	Definition
	File string
}

// Paths returns the indexed file paths, sorted.
func (ix *Index) Paths() []string {
	if ix == nil {
		return nil
	}
	out := make([]string, 0, len(ix.files))
	for p := range ix.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of indexed files.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	return len(ix.files)
}

// DefinitionsInPath returns the definitions recorded for one file.
func (ix *Index) DefinitionsInPath(path string) []Definition {
	if ix == nil {
		return nil
	}
	f := ix.files[filepath.ToSlash(path)]
	if f == nil {
		return nil
	}
	return f.Defs
}

// ReferencesInPath returns the references recorded for one file.
func (ix *Index) ReferencesInPath(path string) []Reference {
	if ix == nil {
		return nil
	}
	f := ix.files[filepath.ToSlash(path)]
	if f == nil {
		return nil
	}
	return f.Refs
}

// DefinitionsOf returns every definition of a name (case-insensitive), keyed by
// the last dot-separated segment so qualified and unqualified names match.
func (ix *Index) DefinitionsOf(name string) []LocatedDefinition {
	if ix == nil {
		return nil
	}
	base := strings.ToLower(Base(name))
	var out []LocatedDefinition
	for _, p := range ix.Paths() {
		for _, d := range ix.files[p].Defs {
			if d.Base() == base {
				out = append(out, LocatedDefinition{Definition: d, File: p})
			}
		}
	}
	return out
}

// DefinitionFiles returns the sorted paths that define a name.
func (ix *Index) DefinitionFiles(name string) []string {
	if ix == nil {
		return nil
	}
	base := strings.ToLower(Base(name))
	var out []string
	for _, p := range ix.Paths() {
		for _, d := range ix.files[p].Defs {
			if d.Base() == base {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// IsReferenced reports whether any indexed file references a name.
func (ix *Index) IsReferenced(name string) bool {
	if ix == nil {
		return false
	}
	base := strings.ToLower(Base(name))
	for _, p := range ix.Paths() {
		for _, r := range ix.files[p].Refs {
			if Base(r.Name) == base {
				return true
			}
		}
	}
	return false
}

// ReferencedElsewhere reports whether a name is referenced by a file other than
// path.
func (ix *Index) ReferencedElsewhere(path, name string) bool {
	if ix == nil {
		return false
	}
	self := filepath.ToSlash(path)
	base := strings.ToLower(Base(name))
	for _, p := range ix.Paths() {
		if p == self {
			continue
		}
		for _, r := range ix.files[p].Refs {
			if Base(r.Name) == base {
				return true
			}
		}
	}
	return false
}

// --- persistence -----------------------------------------------------------

func storePath(opts Options) string {
	if opts.CacheDir == "" {
		return ""
	}
	return filepath.Join(opts.CacheDir, FileName)
}

func loadStore(opts Options) *store {
	if opts.NoCache {
		return nil
	}
	path := storePath(opts)
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st store
	if err := json.Unmarshal(b, &st); err != nil {
		return nil
	}
	if st.Version != Version || st.Settings != opts.Settings.DialectName {
		return nil
	}
	if st.Files == nil {
		st.Files = map[string]*facts{}
	}
	return &st
}

func saveStore(opts Options, st *store) {
	if opts.NoCache || st == nil {
		return
	}
	path := storePath(opts)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
