# Changelog

## 0.1.2

- Tracks the `rusq v0.1.2` release. `rusq server` now accepts the `--stdio`
  and `--clientProcessId` flags that `vscode-languageclient` appends when it
  launches the server over stdio, fixing `write EPIPE` on startup.
- Platform packages (`linux-x64`, `linux-arm64`, `darwin-x64`, `darwin-arm64`,
  `win32-x64`, `win32-arm64`) bundle the matching `rusq` binary, so installing
  from the Marketplace or Open VSX needs no separate setup. The generic package
  keeps the old behaviour and uses a `rusq` on `PATH`.
- Added the extension icon, homepage, issue tracker and gallery banner.

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
