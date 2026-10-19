package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/ppreeper/rusq/internal/cache"
	"github.com/ppreeper/rusq/internal/config"
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/fix"
	"github.com/ppreeper/rusq/internal/formatter"
	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/rules"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/version"
	"github.com/ppreeper/rusq/internal/walk"
)

// Options configures the server. They mirror the CLI's server flags.
type Options struct {
	// Config pins a single configuration file.
	Config string
	// Isolated disables configuration discovery.
	Isolated bool
	// Dialect is the fallback dialect when a file's config does not set one.
	Dialect string
	// Templater is the fallback templater.
	Templater string
	// CacheDir overrides the result cache location.
	CacheDir string
	// NoCache disables the result cache.
	NoCache bool
	// RootDir is the fallback directory for configuration discovery when a
	// document has no path on disk (e.g. an untitled buffer).
	RootDir string
}

// document is an open buffer.
type document struct {
	uri     string
	path    string
	version int
	text    []byte
}

// Server is an LSP server instance.
type Server struct {
	opts Options
	in   io.Reader
	out  io.Writer
	log  *log.Logger

	mu            sync.Mutex
	writeMu       sync.Mutex
	docs          map[string]*document
	settingsCache map[string]settings.Settings
	cache         *cache.Cache

	projIndex    *index.Index
	projIndexKey string

	rootDir     string
	shutdown    bool
	exit        bool
	initialized bool
}

// Serve runs a server over stdio using the process environment.
func Serve(in io.Reader, out io.Writer, opts Options) error {
	return NewServer(opts, in, out, loggerFromEnv()).Serve()
}

// NewServer builds a server. A nil logger discards output.
func NewServer(opts Options, in io.Reader, out io.Writer, lg *log.Logger) *Server {
	if lg == nil {
		lg = log.New(io.Discard, "", 0)
	}
	s := &Server{
		opts:          opts,
		in:            in,
		out:           out,
		log:           lg,
		docs:          map[string]*document{},
		settingsCache: map[string]settings.Settings{},
		rootDir:       opts.RootDir,
	}
	if !opts.NoCache {
		s.cache = cache.New(opts.CacheDir)
	}
	return s
}

// loggerFromEnv returns a stderr logger when RUSQ_LOG is set.
func loggerFromEnv() *log.Logger {
	if os.Getenv("RUSQ_LOG") == "" {
		return log.New(io.Discard, "", 0)
	}
	return log.New(os.Stderr, "rusq-lsp: ", log.LstdFlags)
}

// Serve reads messages until the client exits or the stream closes.
func (s *Server) Serve() error {
	s.log.Printf("serving on stdio (root %q)", s.rootDir)
	defer s.log.Printf("stopped")
	r := bufio.NewReader(s.in)
	for {
		m, err := readMessage(r)
		if err != nil {
			if errors.Is(err, io.EOF) || s.exit {
				return nil
			}
			return err
		}
		if len(m.ID) > 0 && string(m.ID) != "null" {
			result, rerr := s.dispatch(m)
			if rerr != nil {
				s.log.Printf("error %s: %s", m.Method, rerr.Message)
				_ = s.write(&message{JSONRPC: "2.0", ID: m.ID, Error: rerr})
				continue
			}
			raw, err := json.Marshal(result)
			if err != nil {
				_ = s.write(&message{JSONRPC: "2.0", ID: m.ID, Error: errorf(codeInternalError, "marshal: %v", err)})
				continue
			}
			if result == nil {
				raw = json.RawMessage("null")
			}
			_ = s.write(&message{JSONRPC: "2.0", ID: m.ID, Result: raw})
			continue
		}
		s.dispatchNotify(m)
		if s.exit {
			return nil
		}
	}
}

func (s *Server) write(m *message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return writeMessage(s.out, m)
}

func (s *Server) notify(method string, params any) {
	raw, err := json.Marshal(params)
	if err != nil {
		s.log.Printf("marshal %s: %v", method, err)
		return
	}
	if err := s.write(&message{JSONRPC: "2.0", Method: method, Params: raw}); err != nil {
		s.log.Printf("write %s: %v", method, err)
	}
}

// dispatch handles a request.
func (s *Server) dispatch(m *message) (any, *responseError) {
	switch m.Method {
	case "initialize":
		return s.handleInitialize(m.Params)
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		return nil, nil
	case "textDocument/formatting":
		var p documentFormattingParams
		if err := unmarshalParams(m.Params, &p); err != nil {
			return nil, err
		}
		return s.formatting(p.TextDocument.URI, nil), nil
	case "textDocument/rangeFormatting":
		var p documentRangeFormattingParams
		if err := unmarshalParams(m.Params, &p); err != nil {
			return nil, err
		}
		return s.formatting(p.TextDocument.URI, &p.Range), nil
	case "textDocument/codeAction":
		var p codeActionParams
		if err := unmarshalParams(m.Params, &p); err != nil {
			return nil, err
		}
		return s.codeActions(p.TextDocument.URI, p.Range, p.Context.Only), nil
	default:
		return nil, errorf(codeMethodNotFound, "method not found: %s", m.Method)
	}
}

// dispatchNotify handles a notification.
func (s *Server) dispatchNotify(m *message) {
	switch m.Method {
	case "initialized":
		s.mu.Lock()
		s.initialized = true
		s.mu.Unlock()
	case "exit":
		s.mu.Lock()
		s.exit = true
		s.mu.Unlock()
	case "textDocument/didOpen":
		var p didOpenParams
		if unmarshalParams(m.Params, &p) != nil {
			return
		}
		s.didOpen(p.TextDocument)
	case "textDocument/didChange":
		var p didChangeParams
		if unmarshalParams(m.Params, &p) != nil {
			return
		}
		s.didChange(p.TextDocument.URI, p.TextDocument.Version, p.ContentChanges)
	case "textDocument/didSave":
		var p didSaveParams
		if unmarshalParams(m.Params, &p) != nil {
			return
		}
		s.invalidateIndex()
		s.publish(p.TextDocument.URI)
	case "textDocument/didClose":
		var p didCloseParams
		if unmarshalParams(m.Params, &p) != nil {
			return
		}
		s.mu.Lock()
		delete(s.docs, p.TextDocument.URI)
		s.mu.Unlock()
		s.invalidateIndex()
		// Clear the client's diagnostics for the closed document.
		s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{
			URI:         p.TextDocument.URI,
			Diagnostics: []lspDiagnostic{},
		})
	case "workspace/didChangeConfiguration":
		// Settings may have changed (including whether the index is enabled).
		s.mu.Lock()
		s.settingsCache = map[string]settings.Settings{}
		s.mu.Unlock()
		s.invalidateIndex()
	case "workspace/didChangeWatchedFiles":
		s.invalidateIndex()
	case "$/cancelRequest", "textDocument/willSave", "textDocument/willSaveWaitUntil":
		// Notifications rusq ignores.
	default:
		s.log.Printf("ignoring notification %s", m.Method)
	}
}

func (s *Server) handleInitialize(raw json.RawMessage) (any, *responseError) {
	var p initializeParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errorf(codeInvalidRequest, "initialize: %v", err)
		}
	}
	root := s.opts.RootDir
	if root == "" {
		if path := pathFromURI(p.RootURI); path != "" {
			root = path
		} else if p.RootPath != "" {
			root = p.RootPath
		}
	}
	s.mu.Lock()
	s.rootDir = root
	s.mu.Unlock()
	s.log.Printf("initialized by %s %s root=%q", p.ClientInfo.Name, p.ClientInfo.Version, root)

	return initializeResult{
		Capabilities: serverCapabilities{
			TextDocumentSync:                textDocumentSyncOptions{OpenClose: true, Change: SyncKindFull, Save: &saveOptions{}},
			DocumentFormattingProvider:      true,
			DocumentRangeFormattingProvider: true,
			CodeActionProvider: codeActionOptions{
				CodeActionKinds: []string{"quickfix", "source.fixAll.rusq"},
			},
		},
		ServerInfo: serverInfo{Name: "rusq", Version: version.String()},
	}, nil
}

func unmarshalParams(raw json.RawMessage, v any) *responseError {
	if len(raw) == 0 {
		return errorf(codeInvalidRequest, "missing params")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return errorf(codeInvalidRequest, "%v", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Documents
// ---------------------------------------------------------------------------

func (s *Server) didOpen(item textDocumentItem) {
	s.mu.Lock()
	s.docs[item.URI] = &document{
		uri:     item.URI,
		path:    pathFromURI(item.URI),
		version: item.Version,
		text:    []byte(item.Text),
	}
	s.mu.Unlock()
	s.publish(item.URI)
}

func (s *Server) didChange(uri string, version int, changes []contentChange) {
	if len(changes) == 0 {
		return
	}
	s.mu.Lock()
	doc, ok := s.docs[uri]
	if !ok {
		s.mu.Unlock()
		return
	}
	// The server advertises full sync, so the last change carries the whole
	// document. Tolerate range-based changes by applying them in order.
	text := doc.text
	for _, ch := range changes {
		if ch.Range == nil {
			text = []byte(ch.Text)
			continue
		}
		loc := source.NewLocator(text)
		start := loc.Offset(source.Pos{Line: ch.Range.Start.Line + 1, Col: ch.Range.Start.Character + 1})
		end := loc.Offset(source.Pos{Line: ch.Range.End.Line + 1, Col: ch.Range.End.Character + 1})
		if start < 0 || end < start || end > len(text) {
			continue
		}
		next := make([]byte, 0, len(text)-(end-start)+len(ch.Text))
		next = append(next, text[:start]...)
		next = append(next, ch.Text...)
		next = append(next, text[end:]...)
		text = next
	}
	doc.text = text
	doc.version = version
	s.mu.Unlock()
	s.publish(uri)
}

// doc returns a snapshot of the document.
func (s *Server) doc(uri string) *document {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[uri]
	if !ok {
		return nil
	}
	cp := *d
	return &cp
}

// publish lints a document and publishes diagnostics for its current version.
// Results computed for an older version are dropped.
func (s *Server) publish(uri string) {
	doc := s.doc(uri)
	if doc == nil {
		return
	}
	version := doc.version
	st := s.settingsFor(doc.path)
	ds := s.lint(doc.path, doc.text, st)

	// Drop stale results: the document may have changed while linting.
	if cur := s.doc(uri); cur == nil || cur.version != version {
		s.log.Printf("dropping stale diagnostics for %s (version %d)", uri, version)
		return
	}
	loc := source.NewLocator(doc.text)
	s.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{
		URI:         uri,
		Version:     version,
		Diagnostics: toLSPDiagnostics(loc, ds),
	})
}

func (s *Server) lint(path string, text []byte, st settings.Settings) []diagnostic.Diagnostic {
	ix := s.indexFor(st)
	if ix != nil && path != "" {
		// The editor buffer may be ahead of the file on disk. Recording the
		// buffer here means cross-file rules see exactly what the user sees.
		ix.Put(path, text, st.Dialect)
	}
	// The result cache is keyed by this file's content alone, but a change to
	// another file can change cross-file findings, so an active index bypasses
	// the cache.
	if s.cache != nil && path != "" && ix == nil {
		key := cache.Key(path, text, cache.SettingsHash(st))
		if ds, ok := s.cache.Get(key); ok {
			return ds
		}
		l := linter.New(st)
		res := l.Run(source.New(path, text), linter.Options{Index: ix})
		s.cache.Put(key, res.Diagnostics)
		return res.Diagnostics
	}
	l := linter.New(st)
	return l.Run(source.New(path, text), linter.Options{Index: ix}).Diagnostics
}

// indexFor returns the project index for the server root, building it lazily on
// the first lint that needs it. It returns nil when cross-file analysis is
// disabled ([index] enabled is false) or no root directory is known.
func (s *Server) indexFor(st settings.Settings) *index.Index {
	if !st.Index {
		return nil
	}
	s.mu.Lock()
	root := s.rootDir
	if s.projIndex != nil && s.projIndexKey == root {
		ix := s.projIndex
		s.mu.Unlock()
		return ix
	}
	s.mu.Unlock()
	if root == "" {
		return nil
	}
	res, err := walk.Discover(walk.Options{
		Roots:            []string{root},
		Exts:             st.FileExts,
		RespectGitignore: true,
	})
	if err != nil {
		s.log.Printf("index: %v", err)
		return nil
	}
	ix, err := index.Build(res.Files, index.Options{
		Settings: st,
		CacheDir: s.indexCacheDir(root),
		NoCache:  s.opts.NoCache,
	})
	if err != nil {
		s.log.Printf("index: %v", err)
		return nil
	}
	s.mu.Lock()
	s.projIndex, s.projIndexKey = ix, root
	s.mu.Unlock()
	s.log.Printf("indexed %d files under %s", ix.Len(), root)
	return ix
}

func (s *Server) indexCacheDir(root string) string {
	if s.opts.CacheDir != "" {
		return s.opts.CacheDir
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, cache.DirName)
}

// invalidateIndex drops the cached project index so the next lint rebuilds it
// from disk. Called when a file is saved, closed, or changed outside the editor.
func (s *Server) invalidateIndex() {
	s.mu.Lock()
	s.projIndex, s.projIndexKey = nil, ""
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// settingsFor resolves the effective settings for a path, memoised per
// directory so repeated edits do not re-walk the filesystem.
func (s *Server) settingsFor(path string) settings.Settings {
	dir := s.baseDir(path)
	s.mu.Lock()
	if st, ok := s.settingsCache[dir]; ok {
		s.mu.Unlock()
		return st
	}
	root := s.rootDir
	s.mu.Unlock()

	st, err := resolveSettings(dir, root, s.opts)
	if err != nil {
		s.log.Printf("config error for %s: %v", dir, err)
		st = settings.Default()
	}
	s.mu.Lock()
	s.settingsCache[dir] = st
	s.mu.Unlock()
	return st
}

func (s *Server) baseDir(path string) string {
	if path != "" {
		return filepath.Dir(path)
	}
	s.mu.Lock()
	root := s.rootDir
	s.mu.Unlock()
	if root != "" {
		return root
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func resolveSettings(dir, root string, opts Options) (settings.Settings, error) {
	start := dir
	if start == "" {
		start = root
	}
	if start == "" {
		start = "."
	}
	c, files, err := config.LoadChain(start, opts.Config, opts.Isolated)
	if err != nil {
		return settings.Settings{}, err
	}
	st, err := settings.FromConfig(c, start, files)
	if err != nil {
		return st, err
	}
	if opts.Dialect != "" {
		d, ok := dialect.Lookup(opts.Dialect)
		if !ok {
			return st, fmt.Errorf("unknown dialect %q", opts.Dialect)
		}
		st.DialectName = d.Name
		st.Dialect = d
	}
	if opts.Templater != "" {
		st.Templater = opts.Templater
	}
	return st, nil
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// formatting returns the edits that format a document (optionally a range).
func (s *Server) formatting(uri string, r *lspRange) []textEdit {
	doc := s.doc(uri)
	if doc == nil {
		return []textEdit{}
	}
	st := s.settingsFor(doc.path)
	f := formatter.New(st)
	if r != nil {
		f.StartLine = r.Start.Line + 1
		f.EndLine = r.End.Line + 1
	}
	out := f.FormatBytes(doc.text)
	if bytes.Equal(out, doc.text) {
		return []textEdit{}
	}
	edit := diffEdit(doc.text, out)
	if edit == nil {
		return []textEdit{}
	}
	return []textEdit{*edit}
}

// diffEdit returns a single edit covering the lines that changed between old
// and new. Formatting only ever changes whole lines, so a line-window diff is
// both minimal and exact.
func diffEdit(old, new []byte) *textEdit {
	loc := source.NewLocator(old)
	oldLines := strings.Split(string(old), "\n")
	newLines := strings.Split(string(new), "\n")

	p := 0
	for p < len(oldLines) && p < len(newLines) && oldLines[p] == newLines[p] {
		p++
	}
	sfx := 0
	for sfx < len(oldLines)-p && sfx < len(newLines)-p &&
		oldLines[len(oldLines)-1-sfx] == newLines[len(newLines)-1-sfx] {
		sfx++
	}
	removed := len(oldLines) - p - sfx
	added := len(newLines) - p - sfx
	if removed == 0 && added == 0 {
		return nil
	}
	startOff := loc.LineStart(p + 1)
	endOff := startOff
	if removed > 0 {
		endOff = loc.LineStart(p + removed + 1)
	}
	var b strings.Builder
	b.WriteString(strings.Join(newLines[p:p+added], "\n"))
	if removed > 0 && added > 0 {
		b.WriteString("\n")
	}
	return &textEdit{
		Range:   lspRange{Start: positionAt(loc, startOff), End: positionAt(loc, endOff)},
		NewText: b.String(),
	}
}

// ---------------------------------------------------------------------------
// Code actions
// ---------------------------------------------------------------------------

func (s *Server) codeActions(uri string, r lspRange, only []string) []codeAction {
	doc := s.doc(uri)
	if doc == nil {
		return []codeAction{}
	}
	st := s.settingsFor(doc.path)
	l := linter.New(st)
	res := l.Run(source.New(doc.path, doc.text), linter.Options{})
	loc := source.NewLocator(doc.text)
	text := doc.text

	var actions []codeAction
	var fixable []diagnostic.Diagnostic
	for _, d := range res.Diagnostics {
		if !rangeOverlaps(loc, d.Span, r) {
			continue
		}
		ld := toLSPDiagnostic(loc, d)

		if d.Fix != nil && d.Fixability == diagnostic.Safe && st.IsFixable(d.Code, d.Name, ruleFamily(d.Code), ruleCategory(d.Code)) {
			if newBytes, applied := fix.Apply(text, []diagnostic.Diagnostic{d}, false); len(applied) > 0 {
				if edit := diffEdit(text, newBytes); edit != nil {
					actions = append(actions, codeAction{
						Title:       fmt.Sprintf("rusq: fix %s", d.Code),
						Kind:        "quickfix",
						Diagnostics: []lspDiagnostic{ld},
						IsPreferred: true,
						Edit:        &workspaceEdit{Changes: map[string][]textEdit{uri: {*edit}}},
					})
				}
				fixable = append(fixable, d)
			}
		}

		if act, ok := s.suppressAction(uri, text, d, ld, loc, st); ok {
			actions = append(actions, act)
		}
	}

	// A single "fix all safe fixes" action, offered whenever there is work.
	if len(fixable) > 0 {
		if fres := l.Run(source.New(doc.path, text), linter.Options{Fix: true}); fres.Changed && len(fres.FixedSource) > 0 {
			if edit := diffEdit(text, fres.FixedSource); edit != nil {
				actions = append(actions, codeAction{
					Title: "rusq: fix all auto-fixable problems",
					Kind:  "source.fixAll.rusq",
					Edit:  &workspaceEdit{Changes: map[string][]textEdit{uri: {*edit}}},
				})
			}
		}
	}

	return filterKinds(actions, only)
}

// suppressAction builds a quickfix that inserts the directive which silences d.
func (s *Server) suppressAction(uri string, text []byte, d diagnostic.Diagnostic, ld lspDiagnostic, loc *source.Locator, st settings.Settings) (codeAction, bool) {
	if d.Code == "" || d.Code == "RUS101" || d.Code == "RUS102" {
		return codeAction{}, false
	}
	line := loc.Position(d.Span.Start).Line
	lineStart := loc.LineStart(line)
	lineEnd := loc.LineEnd(line)
	lineText := string(text[lineStart:lineEnd])

	var edit textEdit
	if hasLineComment(text, line, st) {
		// The line already carries a comment; add a directive on its own line
		// above, which covers the following statement.
		insert := "-- rusq: skip=" + d.Code + "\n" + leadingWhitespace(lineText)
		edit = textEdit{
			Range:   lspRange{Start: positionAt(loc, lineStart), End: positionAt(loc, lineStart)},
			NewText: insert,
		}
	} else {
		edit = textEdit{
			Range:   lspRange{Start: positionAt(loc, lineEnd), End: positionAt(loc, lineEnd)},
			NewText: " -- rusq: " + d.Code,
		}
	}
	return codeAction{
		Title:       "rusq: suppress " + d.Code + " on this line",
		Kind:        "quickfix",
		Diagnostics: []lspDiagnostic{ld},
		Edit:        &workspaceEdit{Changes: map[string][]textEdit{uri: {edit}}},
	}, true
}

func filterKinds(actions []codeAction, only []string) []codeAction {
	if len(only) == 0 {
		return actions
	}
	var out []codeAction
	for _, a := range actions {
		for _, k := range only {
			if a.Kind == k || strings.HasPrefix(a.Kind, k+".") || strings.HasPrefix(k, a.Kind+".") {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// hasLineComment reports whether a line carries a line comment token.
func hasLineComment(text []byte, line int, st settings.Settings) bool {
	toks, _ := lexer.Lex(source.New("", text), st.Dialect)
	for _, t := range toks {
		if t.Kind == lexer.Comment && t.Line == line {
			return true
		}
	}
	return false
}

func leadingWhitespace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}

// ---------------------------------------------------------------------------
// Conversions
// ---------------------------------------------------------------------------

func toLSPDiagnostics(loc *source.Locator, ds []diagnostic.Diagnostic) []lspDiagnostic {
	out := make([]lspDiagnostic, 0, len(ds))
	for _, d := range ds {
		out = append(out, toLSPDiagnostic(loc, d))
	}
	return out
}

func toLSPDiagnostic(loc *source.Locator, d diagnostic.Diagnostic) lspDiagnostic {
	ld := lspDiagnostic{
		Range:    lspRange{Start: positionAt(loc, d.Span.Start), End: positionAt(loc, d.Span.End)},
		Severity: severityOf(d.Severity),
		Code:     d.Code,
		Source:   "rusq",
		Message:  d.Message,
	}
	data := map[string]any{}
	if d.Name != "" {
		data["rule"] = d.Name
	}
	if len(d.Provenance) > 0 {
		data["provenance"] = d.Provenance
	}
	data["fixable"] = d.Fix != nil
	switch d.Fixability {
	case diagnostic.Safe:
		data["fixability"] = "safe"
	case diagnostic.Unsafe:
		data["fixability"] = "unsafe"
	case diagnostic.Display:
		data["fixability"] = "display"
	default:
		data["fixability"] = "none"
	}
	if len(d.Related) > 0 {
		related := make([]map[string]any, 0, len(d.Related))
		for _, rel := range d.Related {
			related = append(related, map[string]any{
				"message": rel.Message,
				"range":   lspRange{Start: positionAt(loc, rel.Span.Start), End: positionAt(loc, rel.Span.End)},
			})
		}
		data["related"] = related
	}
	if len(data) > 0 {
		ld.Data = data
	}
	return ld
}

// ruleFamily and ruleCategory look up a rule's metadata for selection checks.
func ruleFamily(code string) string {
	if r, ok := rules.ByCode(code); ok {
		return r.Meta().Family
	}
	return ""
}

func ruleCategory(code string) string {
	if r, ok := rules.ByCode(code); ok {
		return string(r.Meta().Category)
	}
	return ""
}

func severityOf(s diagnostic.Severity) int {
	switch s {
	case diagnostic.Error:
		return 1
	case diagnostic.Warning:
		return 2
	case diagnostic.Hint:
		return 4
	default:
		return 3
	}
}

// positionAt converts a byte offset to an LSP position (0-based line, UTF-16
// code-unit character offset).
func positionAt(loc *source.Locator, off int) position {
	p := loc.Position(off)
	return position{
		Line:      p.Line - 1,
		Character: utf16Column(loc.Src(), loc.LineStart(p.Line), off),
	}
}

func utf16Column(src []byte, lineStart, off int) int {
	if off > len(src) {
		off = len(src)
	}
	if off < lineStart {
		off = lineStart
	}
	n := 0
	for i := lineStart; i < off; {
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size <= 1 {
			n++
			i++
			continue
		}
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
		i += size
	}
	return n
}

func rangeOverlaps(loc *source.Locator, span source.Range, r lspRange) bool {
	start := loc.Offset(source.Pos{Line: r.Start.Line + 1, Col: r.Start.Character + 1})
	end := loc.Offset(source.Pos{Line: r.End.Line + 1, Col: r.End.Character + 1})
	if end < start {
		start, end = end, start
	}
	// A zero-width request range (a cursor) matches the whole containing line,
	// so a quick fix is offered wherever the cursor sits on the offending line.
	if start == end {
		ls := loc.LineStart(r.Start.Line + 1)
		le := loc.LineEnd(r.Start.Line + 1)
		return span.Start < le && ls < span.End
	}
	return span.Start < end && start < span.End
}

// pathFromURI converts a file:// URI to a filesystem path.
func pathFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	if !strings.HasPrefix(uri, "file:") {
		return ""
	}
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" {
		p = u.Opaque
	}
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	if p == "" {
		return ""
	}
	return filepath.FromSlash(p)
}
