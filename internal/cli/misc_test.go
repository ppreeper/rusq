package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
)

func lspFrame(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

// vscode-languageclient v10 appends `--stdio --clientProcessId=<pid>` when it
// launches an Executable server over stdio. The server must accept and ignore
// both instead of exiting with `unknown flag: --stdio`.
func TestServerAcceptsLSPClientFlags(t *testing.T) {
	var frames strings.Builder
	frames.WriteString(lspFrame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	frames.WriteString(lspFrame(`{"jsonrpc":"2.0","method":"initialized","params":{}}`))
	frames.WriteString(lspFrame(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`))
	frames.WriteString(lspFrame(`{"jsonrpc":"2.0","method":"exit"}`))

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(frames.String()))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"server", "--stdio", "--clientProcessId=1234"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("server with LSP client flags: %v (stderr %q)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "textDocumentSync") {
		t.Fatalf("server did not initialize: %q", stdout.String())
	}
}

// The compatibility flags belong to `server`; every other subcommand must
// still reject them.
func TestLSPClientFlagsRejectedElsewhere(t *testing.T) {
	for _, args := range [][]string{
		{"version", "--stdio"},
		{"version", "--clientProcessId=1234"},
	} {
		cmd := NewRootCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("%v: expected an unknown-flag error", args)
		}
	}
}
