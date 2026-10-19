// Package lsp implements a Language Server Protocol server for rusq.
//
// The server speaks LSP over stdio and is a thin adapter over the same library
// the CLI uses: configuration discovery, templating, lexing, parsing, rules,
// the fix engine and the formatter are all shared, so editor diagnostics and
// CI diagnostics agree exactly.
package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// message is a JSON-RPC 2.0 envelope.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func errorf(code int, format string, args ...any) *responseError {
	return &responseError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// readMessage reads one Content-Length framed message.
func readMessage(r *bufio.Reader) (*message, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if line == "" {
				return nil, err
			}
			return nil, fmt.Errorf("reading header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("message without Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// writeMessage writes one Content-Length framed message.
func writeMessage(w io.Writer, m *message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// ---------------------------------------------------------------------------
// LSP structures (only the subset rusq uses)
// ---------------------------------------------------------------------------

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type contentChange struct {
	Range *lspRange `json:"range,omitempty"`
	Text  string    `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []contentChange                 `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type formatOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

type documentFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Options      formatOptions          `json:"options"`
}

type documentRangeFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        lspRange               `json:"range"`
	Options      formatOptions          `json:"options"`
}

type codeActionContext struct {
	Diagnostics []lspDiagnostic `json:"diagnostics"`
	Only        []string        `json:"only,omitempty"`
}

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        lspRange               `json:"range"`
	Context      codeActionContext      `json:"context"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

type workspaceEdit struct {
	Changes map[string][]textEdit `json:"changes,omitempty"`
}

type codeAction struct {
	Title       string          `json:"title"`
	Kind        string          `json:"kind,omitempty"`
	Diagnostics []lspDiagnostic `json:"diagnostics,omitempty"`
	IsPreferred bool            `json:"isPreferred,omitempty"`
	Edit        *workspaceEdit  `json:"edit,omitempty"`
}

// lspDiagnostic is the wire form of a rusq diagnostic.
type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity,omitempty"`
	Code     string   `json:"code,omitempty"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message"`
	Data     any      `json:"data,omitempty"`
}

type publishDiagnosticsParams struct {
	URI         string          `json:"uri"`
	Version     int             `json:"version,omitempty"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

type textDocumentSyncOptions struct {
	OpenClose bool         `json:"openClose"`
	Change    int          `json:"change"`
	Save      *saveOptions `json:"save,omitempty"`
}

type saveOptions struct {
	IncludeText bool `json:"includeText"`
}

type codeActionOptions struct {
	CodeActionKinds []string `json:"codeActionKinds,omitempty"`
}

type serverCapabilities struct {
	TextDocumentSync                textDocumentSyncOptions `json:"textDocumentSync"`
	DocumentFormattingProvider      bool                    `json:"documentFormattingProvider"`
	DocumentRangeFormattingProvider bool                    `json:"documentRangeFormattingProvider"`
	CodeActionProvider              codeActionOptions       `json:"codeActionProvider"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}

type initializeParams struct {
	ProcessID  int    `json:"processId"`
	RootURI    string `json:"rootUri"`
	RootPath   string `json:"rootPath"`
	ClientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

// SyncKindFull is the LSP textDocumentSync change value for full sync.
const SyncKindFull = 1
