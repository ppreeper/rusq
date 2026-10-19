package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

// ---------------------------------------------------------------------------
// Test client
// ---------------------------------------------------------------------------

type client struct {
	t       *testing.T
	w       io.Writer
	r       *bufio.Reader
	nextID  int
	pending []*message
}

func startServer(t *testing.T, opts Options) (*client, func()) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := NewServer(opts, inR, outW, nil)
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	c := &client{t: t, w: inW, r: bufio.NewReader(outR)}
	stop := func() {
		_ = inW.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Errorf("server did not stop")
		}
		_ = outW.Close()
	}
	return c, stop
}

func (c *client) recv() *message {
	c.t.Helper()
	type res struct {
		m   *message
		err error
	}
	ch := make(chan res, 1)
	go func() {
		m, err := readMessage(c.r)
		ch <- res{m, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			c.t.Fatalf("read message: %v", r.err)
		}
		return r.m
	case <-time.After(5 * time.Second):
		c.t.Fatalf("timeout waiting for a message")
		return nil
	}
}

func (c *client) send(m *message) {
	c.t.Helper()
	if err := writeMessage(c.w, m); err != nil {
		c.t.Fatalf("write message: %v", err)
	}
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	c.send(&message{JSONRPC: "2.0", Method: method, Params: raw})
}

func (c *client) request(method string, params any) json.RawMessage {
	c.t.Helper()
	c.nextID++
	raw, err := json.Marshal(params)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	c.send(&message{JSONRPC: "2.0", ID: json.RawMessage(strconv.Itoa(c.nextID)), Method: method, Params: raw})
	for {
		m := c.recv()
		if len(m.ID) > 0 {
			if m.Error != nil {
				c.t.Fatalf("rpc error for %s: %s", method, m.Error.Message)
			}
			return m.Result
		}
		c.pending = append(c.pending, m)
	}
}

func (c *client) recvNotification(method string) *message {
	c.t.Helper()
	for i, m := range c.pending {
		if m.Method == method {
			c.pending = append(c.pending[:i], c.pending[i+1:]...)
			return m
		}
	}
	for {
		m := c.recv()
		if m.Method == method {
			return m
		}
		c.pending = append(c.pending, m)
	}
}

// open opens a document and returns the published diagnostics.
func (c *client) open(uri, text string, version int) publishDiagnosticsParams {
	c.t.Helper()
	c.notify("textDocument/didOpen", didOpenParams{TextDocument: textDocumentItem{
		URI: uri, LanguageID: "sql", Version: version, Text: text,
	}})
	return c.diagnostics()
}

func (c *client) diagnostics() publishDiagnosticsParams {
	c.t.Helper()
	m := c.recvNotification("textDocument/publishDiagnostics")
	var p publishDiagnosticsParams
	if err := json.Unmarshal(m.Params, &p); err != nil {
		c.t.Fatalf("unmarshal diagnostics: %v", err)
	}
	return p
}

// applyEdit applies an LSP edit to text.
func applyEdit(t *testing.T, text []byte, e textEdit) []byte {
	t.Helper()
	loc := source.NewLocator(text)
	start := loc.Offset(source.Pos{Line: e.Range.Start.Line + 1, Col: e.Range.Start.Character + 1})
	end := loc.Offset(source.Pos{Line: e.Range.End.Line + 1, Col: e.Range.End.Character + 1})
	if start > end {
		t.Fatalf("inverted edit range %+v", e.Range)
	}
	out := make([]byte, 0, len(text)-(end-start)+len(e.NewText))
	out = append(out, text[:start]...)
	out = append(out, e.NewText...)
	out = append(out, text[end:]...)
	return out
}

func fileURI(dir, name string) string { return "file://" + filepath.Join(dir, name) }

func testOpts(t *testing.T) Options {
	t.Helper()
	return Options{NoCache: true, RootDir: t.TempDir()}
}

// fullRange is a range that covers the whole document.
func fullRange() lspRange {
	return lspRange{Start: position{Line: 0, Character: 0}, End: position{Line: 1 << 20, Character: 0}}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestInitialize(t *testing.T) {
	dir := t.TempDir()
	c, stop := startServer(t, testOpts(t))
	defer stop()

	raw := c.request("initialize", map[string]any{
		"rootUri":    "file://" + dir,
		"clientInfo": map[string]any{"name": "test", "version": "1.0"},
	})
	var res initializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if res.ServerInfo.Name != "rusq" {
		t.Errorf("server name = %q, want rusq", res.ServerInfo.Name)
	}
	if res.Capabilities.TextDocumentSync.Change != SyncKindFull {
		t.Errorf("change = %d, want full sync (%d)", res.Capabilities.TextDocumentSync.Change, SyncKindFull)
	}
	if !res.Capabilities.TextDocumentSync.OpenClose {
		t.Error("openClose should be enabled")
	}
	if !res.Capabilities.DocumentFormattingProvider || !res.Capabilities.DocumentRangeFormattingProvider {
		t.Error("formatting and rangeFormatting providers should be enabled")
	}
	kinds := res.Capabilities.CodeActionProvider.CodeActionKinds
	if len(kinds) != 2 || kinds[0] != "quickfix" || kinds[1] != "source.fixAll.rusq" {
		t.Errorf("codeActionKinds = %v", kinds)
	}
}

func TestPublishDiagnostics(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	p := c.open(uri, "SELECT a FROM t WHERE a = NULL;\n", 1)
	if p.URI != uri || p.Version != 1 {
		t.Errorf("uri/version = %q/%d", p.URI, p.Version)
	}
	var cv05 *lspDiagnostic
	for i := range p.Diagnostics {
		if p.Diagnostics[i].Code == "CV05" {
			cv05 = &p.Diagnostics[i]
		}
	}
	if cv05 == nil {
		t.Fatalf("no CV05 diagnostic in %+v", p.Diagnostics)
	}
	if cv05.Severity != 1 {
		t.Errorf("severity = %d, want 1 (error; CV05 is a correctness rule)", cv05.Severity)
	}
	if cv05.Source != "rusq" {
		t.Errorf("source = %q, want rusq", cv05.Source)
	}
	if cv05.Range.Start.Line != 0 {
		t.Errorf("start line = %d, want 0", cv05.Range.Start.Line)
	}
	data, _ := cv05.Data.(map[string]any)
	if data["fixability"] != "safe" || data["fixable"] != true {
		t.Errorf("data = %v, want a safe fixable diagnostic", cv05.Data)
	}
	if data["rule"] != "convention.is_null" {
		t.Errorf("rule = %v, want convention.is_null", data["rule"])
	}
}

func TestPublishDiagnosticsClean(t *testing.T) {
	dir := t.TempDir()
	c, stop := startServer(t, testOpts(t))
	defer stop()

	p := c.open(fileURI(dir, "a.sql"), "SELECT a FROM t;\n", 1)
	if len(p.Diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %+v", p.Diagnostics)
	}
}

func TestDidChangeRepublishes(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	c.open(uri, "SELECT a FROM t;\n", 1)
	c.notify("textDocument/didChange", didChangeParams{
		TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Text: "SELECT a FROM t WHERE a = NULL;\n"}},
	})
	p := c.diagnostics()
	if p.Version != 2 {
		t.Errorf("version = %d, want 2", p.Version)
	}
	found := false
	for _, d := range p.Diagnostics {
		if d.Code == "CV05" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CV05 after change, got %+v", p.Diagnostics)
	}
}

func TestFormatting(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	text := "select a,b from t\n"
	c.open(uri, text, 1)

	raw := c.request("textDocument/formatting", documentFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Options:      formatOptions{TabSize: 4, InsertSpaces: true},
	})
	var edits []textEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		t.Fatalf("unmarshal edits: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(edits))
	}
	got := string(applyEdit(t, []byte(text), edits[0]))
	// The formatter is layout-only: it never changes token case or adds a
	// terminator, it only lays the statement out.
	if got != "select a, b\nfrom t\n" {
		t.Errorf("formatted output = %q, want %q", got, "select a, b\nfrom t\n")
	}
	// The formatted result must be stable.
	c.notify("textDocument/didChange", didChangeParams{
		TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Text: got}},
	})
	c.diagnostics()
	raw2 := c.request("textDocument/formatting", documentFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
	})
	var edits2 []textEdit
	if err := json.Unmarshal(raw2, &edits2); err != nil {
		t.Fatalf("unmarshal edits: %v", err)
	}
	if len(edits2) != 0 {
		t.Errorf("already-formatted document produced edits: %+v", edits2)
	}
}

func TestRangeFormatting(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	text := "select a,  \nfrom t\n"
	c.open(uri, text, 1)

	raw := c.request("textDocument/rangeFormatting", documentRangeFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        lspRange{Start: position{Line: 0, Character: 0}, End: position{Line: 0, Character: 0}},
	})
	var edits []textEdit
	if err := json.Unmarshal(raw, &edits); err != nil {
		t.Fatalf("unmarshal edits: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %d, want 1", len(edits))
	}
	if edits[0].Range.Start.Line != 0 {
		t.Errorf("range = %+v, want an edit starting on line 0", edits[0].Range)
	}
	got := string(applyEdit(t, []byte(text), edits[0]))
	if got != "select a,\nfrom t\n" {
		t.Errorf("range format = %q, want trailing whitespace trimmed on line 0 only", got)
	}
}

func TestCodeActions(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	text := "SELECT a FROM t WHERE a = NULL;\n"
	c.open(uri, text, 1)

	raw := c.request("textDocument/codeAction", codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        fullRange(),
	})
	var actions []codeAction
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	var quickfix, suppress, fixAll *codeAction
	for i := range actions {
		switch {
		case actions[i].Kind == "source.fixAll.rusq":
			fixAll = &actions[i]
		case strings.Contains(actions[i].Title, "suppress"):
			suppress = &actions[i]
		case actions[i].Kind == "quickfix":
			quickfix = &actions[i]
		}
	}
	if quickfix == nil {
		t.Fatalf("no quickfix action in %+v", actions)
	}
	if fixAll == nil {
		t.Fatalf("no source.fixAll.rusq action in %+v", actions)
	}
	if suppress == nil {
		t.Fatalf("no suppress action in %+v", actions)
	}
	if !quickfix.IsPreferred {
		t.Error("quickfix should be preferred")
	}
	edits := quickfix.Edit.Changes[uri]
	if len(edits) != 1 {
		t.Fatalf("quickfix edits = %d, want 1", len(edits))
	}
	got := string(applyEdit(t, []byte(text), edits[0]))
	if !strings.Contains(got, "IS NULL") {
		t.Errorf("quickfix did not rewrite the comparison:\n%s", got)
	}
	if !strings.Contains(quickfix.Title, "CV05") {
		t.Errorf("quickfix title = %q, want the rule code", quickfix.Title)
	}
}

func TestCodeActionAtCursorOnLine(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	c.open(uri, "SELECT a FROM t WHERE a = NULL;\n", 1)

	// A zero-width range (a bare cursor) at the start of the line should still
	// offer the fix for a diagnostic later on that same line.
	raw := c.request("textDocument/codeAction", codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range: lspRange{
			Start: position{Line: 0, Character: 0},
			End:   position{Line: 0, Character: 0},
		},
	})
	var actions []codeAction
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("cursor on the offending line should offer actions")
	}

	// A cursor on a line without diagnostics offers nothing.
	clean := fileURI(dir, "b.sql")
	c.open(clean, "SELECT a FROM t;\n", 1)
	raw = c.request("textDocument/codeAction", codeActionParams{
		TextDocument: textDocumentIdentifier{URI: clean},
		Range: lspRange{
			Start: position{Line: 0, Character: 0},
			End:   position{Line: 0, Character: 0},
		},
	})
	actions = nil
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("clean line offered %d actions", len(actions))
	}
}

func TestCodeActionOnlyFilter(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	c.open(uri, "SELECT a FROM t WHERE a = NULL;\n", 1)
	raw := c.request("textDocument/codeAction", codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        fullRange(),
		Context:      codeActionContext{Only: []string{"source.fixAll.rusq"}},
	})
	var actions []codeAction
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	for _, a := range actions {
		if a.Kind != "source.fixAll.rusq" {
			t.Errorf("action %q returned for an only=source.fixAll.rusq request", a.Kind)
		}
	}
}

func TestSuppressActionIsEffective(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	text := "SELECT a FROM t WHERE a = NULL;\n"
	c.open(uri, text, 1)
	raw := c.request("textDocument/codeAction", codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        fullRange(),
	})
	var actions []codeAction
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	var suppress *codeAction
	for i := range actions {
		if strings.Contains(actions[i].Title, "suppress CV05") {
			suppress = &actions[i]
		}
	}
	if suppress == nil {
		t.Fatalf("no suppress action in %+v", actions)
	}
	edits := suppress.Edit.Changes[uri]
	if len(edits) != 1 {
		t.Fatalf("suppress edits = %d, want 1", len(edits))
	}
	suppressed := applyEdit(t, []byte(text), edits[0])
	if !strings.Contains(string(suppressed), "-- rusq: CV05") {
		t.Fatalf("directive not inserted:\n%s", suppressed)
	}

	// The directive must actually silence the rule.
	res := linter.New(settings.Default()).Run(source.New(fileURI(dir, "a.sql"), suppressed), linter.Options{})
	for _, d := range res.Diagnostics {
		if d.Code == "CV05" {
			t.Errorf("CV05 still reported after suppression:\n%s", suppressed)
		}
	}
}

func TestSuppressActionWithExistingComment(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	text := "SELECT a FROM t WHERE a = NULL; -- legacy\n"
	c.open(uri, text, 1)
	raw := c.request("textDocument/codeAction", codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        fullRange(),
	})
	var actions []codeAction
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	var suppress *codeAction
	for i := range actions {
		if strings.Contains(actions[i].Title, "suppress CV05") {
			suppress = &actions[i]
		}
	}
	if suppress == nil {
		t.Fatalf("no suppress action in %+v", actions)
	}
	edits := suppress.Edit.Changes[uri]
	suppressed := applyEdit(t, []byte(text), edits[0])
	if !strings.Contains(string(suppressed), "-- rusq: skip=CV05") {
		t.Fatalf("skip directive not inserted:\n%s", suppressed)
	}
	res := linter.New(settings.Default()).Run(source.New(fileURI(dir, "a.sql"), suppressed), linter.Options{})
	for _, d := range res.Diagnostics {
		if d.Code == "CV05" {
			t.Errorf("CV05 still reported after suppression:\n%s", suppressed)
		}
	}
}

func TestDidCloseClearsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "a.sql")
	c, stop := startServer(t, testOpts(t))
	defer stop()

	c.open(uri, "SELECT a FROM t WHERE a = NULL;\n", 1)
	c.notify("textDocument/didClose", didCloseParams{TextDocument: textDocumentIdentifier{URI: uri}})
	p := c.diagnostics()
	if len(p.Diagnostics) != 0 {
		t.Errorf("expected cleared diagnostics on close, got %+v", p.Diagnostics)
	}
}

func TestShutdownExit(t *testing.T) {
	c, stop := startServer(t, testOpts(t))
	defer stop()

	raw := c.request("shutdown", nil)
	if string(raw) != "null" {
		t.Errorf("shutdown result = %s, want null", raw)
	}
	c.notify("exit", nil)
}

func TestUnknownMethod(t *testing.T) {
	c, stop := startServer(t, testOpts(t))
	defer stop()

	c.nextID++
	raw, _ := json.Marshal(map[string]any{})
	c.send(&message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "textDocument/hover", Params: raw})
	m := c.recv()
	if m.Error == nil || m.Error.Code != codeMethodNotFound {
		t.Errorf("expected method-not-found error, got %+v", m)
	}
}

// TestTemplatedDiagnosticsInSourceCoordinates checks that a jinja file is
// linted through its rendered variant and that the finding is reported at the
// position the developer sees in the source file.
func TestTemplatedDiagnosticsInSourceCoordinates(t *testing.T) {
	dir := t.TempDir()
	uri := fileURI(dir, "model.sql")
	opts := testOpts(t)
	opts.Templater = "jinja"
	c, stop := startServer(t, opts)
	defer stop()

	text := "SELECT a FROM t WHERE a = {{ 'NULL' }};\n"
	p := c.open(uri, text, 1)
	var cv05 *lspDiagnostic
	for i := range p.Diagnostics {
		if p.Diagnostics[i].Code == "CV05" {
			cv05 = &p.Diagnostics[i]
		}
	}
	if cv05 == nil {
		t.Fatalf("no CV05 for the templated file: %+v", p.Diagnostics)
	}
	// The comparison lives on source line 0, well before the rendered text's
	// equivalent position would fall.
	if cv05.Range.Start.Line != 0 {
		t.Errorf("start line = %d, want 0 (source coordinates)", cv05.Range.Start.Line)
	}
	if cv05.Range.Start.Character < 20 {
		t.Errorf("start character = %d, want the position of `=` in the source", cv05.Range.Start.Character)
	}
}

func TestPathFromURI(t *testing.T) {
	// pathFromURI returns a native path, so the expectation is native too.
	if want := filepath.FromSlash("/tmp/a.sql"); pathFromURI("file:///tmp/a.sql") != want {
		t.Errorf("pathFromURI = %q, want %q", pathFromURI("file:///tmp/a.sql"), want)
	}
	if got := pathFromURI("untitled:Untitled-1"); got != "" {
		t.Errorf("non-file uri = %q, want empty", got)
	}
	if want := filepath.FromSlash("/tmp/a b.sql"); pathFromURI("file:///tmp/a%20b.sql") != want {
		t.Errorf("escaped uri = %q, want %q", pathFromURI("file:///tmp/a%20b.sql"), want)
	}
}

func TestUTF16Column(t *testing.T) {
	src := []byte("a😀b")
	// "a" = 1 unit, "😀" = 2 units.
	if got := utf16Column(src, 0, len("a")); got != 1 {
		t.Errorf("after a: %d", got)
	}
	if got := utf16Column(src, 0, len("a😀")); got != 3 {
		t.Errorf("after emoji: %d", got)
	}
}

// TestCrossFileIndexOptIn checks that project-wide rules reach the editor only
// when [index] enabled = true, and that the server indexes the root directory.
func TestCrossFileIndexOptIn(t *testing.T) {
	write := func(dir, name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	find := func(t *testing.T, p publishDiagnosticsParams, code string) *lspDiagnostic {
		t.Helper()
		for i := range p.Diagnostics {
			if p.Diagnostics[i].Code == code {
				return &p.Diagnostics[i]
			}
		}
		return nil
	}
	const dup = "CREATE TABLE users (id INT);\n"

	t.Run("enabled", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, ".rusq.toml", "[index]\nenabled = true\n")
		write(dir, "a.sql", dup)
		write(dir, "b.sql", dup)
		c, stop := startServer(t, Options{NoCache: true, RootDir: dir})
		defer stop()

		p := c.open(fileURI(dir, "b.sql"), dup, 1)
		d := find(t, p, "ST13")
		if d == nil {
			t.Fatalf("expected ST13 from the project index, got %+v", p.Diagnostics)
		}
		if d.Source != "rusq" {
			t.Errorf("source = %q, want rusq", d.Source)
		}
		if d.Range.Start.Line != 0 {
			t.Errorf("ST13 on line %d, want 0", d.Range.Start.Line)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		dir := t.TempDir()
		write(dir, "a.sql", dup)
		write(dir, "b.sql", dup)
		c, stop := startServer(t, Options{NoCache: true, RootDir: dir})
		defer stop()

		p := c.open(fileURI(dir, "b.sql"), dup, 1)
		if d := find(t, p, "ST13"); d != nil {
			t.Fatalf("ST13 reported without [index] enabled: %+v", d)
		}
	})
}
