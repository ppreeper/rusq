# Changelog

## 0.1.2

- Tracks the `rusq v0.1.2` release. `rusq server` now accepts the `--stdio`
  and `--clientProcessId` flags that `vscode-languageclient` appends when it
  launches the server over stdio, fixing `write EPIPE` on startup.

## 0.1.1

- Tracks the `rusq v0.1.1` release. No client behaviour changes: the same LSP
  surface, settings and commands.

## 0.1.0

- Initial release: LSP client for `rusq server`.
- Diagnostics for SQL files (push model, full-text sync).
- Quick fixes via code actions, a suppress action, and a fix-all source action.
- Document and range formatting through the rusq formatter.
- Settings: `rusq.binaryPath`, `rusq.configPath`, `rusq.trace.server`.
- Commands: restart the language server, show the output channel.
