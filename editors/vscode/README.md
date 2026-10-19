# rusq for VS Code

SQL diagnostics, quick fixes and formatting, powered by
[`rusq server`](https://github.com/ppreeper/rusq) (the rusq language server).

The extension is a thin LSP client: all linting and formatting happens in the
`rusq` binary you already have, so the extension never goes out of date
relative to the CLI.

## Features

- **Diagnostics as you type** for every SQL file — the same rule catalog the CLI
  ships (the `AL`, `AM`, `CP`, `CV`, `LT`, `RF`, `ST` families, the dialect
  families `PG`, `OR`, `TQ`, and the `RUS` meta rules).
- **Quick fixes** — <kbd>Ctrl</kbd>+<kbd>.</kbd> / <kbd>Cmd</kbd>+<kbd>.</kbd>
  offers each rule's automatic fix, plus a *suppress* action that writes the
  right `-- rusq:` directive for you and a *fix all auto-fixable problems*
  source action.
- **Format document and selection** — <kbd>Shift</kbd>+<kbd>Alt</kbd>+<kbd>F</kbd>
  (or *Format Selection*) runs the rusq formatter, honouring your `.rusq.toml`
  `[format]` options. The formatter is layout-only: it never changes token case
  and never rewrites a template tag.
- **Templated SQL** — a finding inside a `{{ ... }}` tag is reported at the tag's
  position in your source file, not at a line in some rendered output.
- **Per-file configuration** — diagnostics come from `.rusq.toml` discovery, so
  nested projects with different dialects and rule selections behave exactly as
  they do on the command line.

## Requirements

The `rusq` binary must be available. Either:

```sh
go install github.com/ppreeper/rusq/cmd/rusq@latest
```

or build it from a checkout (`go build -o rusq ./cmd/rusq`) and point
`rusq.binaryPath` at it.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `rusq.binaryPath` | `""` | Path to the `rusq` binary; empty uses a binary next to the extension, then `rusq` on `PATH`. |
| `rusq.configPath` | `""` | Passed as `--config <path>`; empty uses `.rusq.toml` discovery. |
| `rusq.trace.server` | `off` | `off` \| `messages` \| `verbose` JSON-RPC tracing. |

Commands: **rusq: Restart language server**, **rusq: Show output channel**.

## Build and install from source

```sh
cd editors/vscode
npm install
npm run build        # bundles src/ into dist/extension.js
npm run compile      # type-check only
npm run package      # produces rusq-sql-0.1.0.vsix
code --install-extension rusq-sql-0.1.0.vsix
```

To hack on the extension, open this folder in VS Code and press <kbd>F5</kbd>
(*Run rusq extension*) — it launches an Extension Development Host with this
folder as the extension.

## Notes

- The client uses full-text synchronisation and the server pushes diagnostics,
  so there is nothing to configure to get started.
- VS Code's built-in SQL support also offers a formatter. To make rusq the
  default, add to your settings:

  ```json
  "[sql]": { "editor.defaultFormatter": "ppreeper.rusq-sql" }
  ```

- Formatting a *selection* is supported through the LSP range-formatting
  request; on the command line the equivalent is
  `rusq format --range start_line:col-end_line:col`.
- Language ids other than VS Code's built-in `sql` come from other extensions
  (`jinja-sql`, `sql-bigquery`, `dbt-sql`, `pgsql`, `mysql`, `plsql`). All of
  them are covered out of the box.

## License

MIT — see [LICENSE](LICENSE).
