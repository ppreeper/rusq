package lsp

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
)

// BenchmarkServerDidChange measures one full notification round trip: a
// didChange in, a publishDiagnostics out. This is the loop an editor drives on
// every keystroke that changes the document (full sync).
func BenchmarkServerDidChange(b *testing.B) {
	dir := b.TempDir()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := NewServer(Options{NoCache: true, RootDir: dir}, inR, outW, nil)
	go func() { _ = srv.Serve() }()
	defer inW.Close()
	defer outW.Close()

	br := bufio.NewReader(outR)
	send := func(m *message) {
		b.Helper()
		if err := writeMessage(inW, m); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
	recv := func() *message {
		b.Helper()
		m, err := readMessage(br)
		if err != nil {
			b.Fatalf("read: %v", err)
		}
		return m
	}

	initParams, _ := json.Marshal(initializeParams{ProcessID: 1, RootURI: "file://" + dir})
	send(&message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize", Params: initParams})
	recv()
	send(&message{JSONRPC: "2.0", Method: "initialized"})

	uri := "file://" + filepath.Join(dir, "bench.sql")
	const text = "SELECT a, b FROM t WHERE a = NULL AND b = NULL;\n"

	openParams, _ := json.Marshal(didOpenParams{TextDocument: textDocumentItem{
		URI: uri, LanguageID: "sql", Version: 1, Text: text,
	}})
	send(&message{JSONRPC: "2.0", Method: "textDocument/didOpen", Params: openParams})
	recv() // the initial publishDiagnostics

	changeParams, _ := json.Marshal(didChangeParams{
		TextDocument:   versionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []contentChange{{Text: text}},
	})

	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		send(&message{JSONRPC: "2.0", Method: "textDocument/didChange", Params: changeParams})
		recv()
	}
}
