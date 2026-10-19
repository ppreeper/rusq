# rusq — Editor & CI integration

> One self-contained binary, two integration surfaces: a **CLI** for CI and
> git hooks, and a **language server** for editors. Both run the same rules over
> the same lossless parser, so an editor and CI can never disagree about a file.

Status: **draft v0.1** — this document defines how `rusq` is wired into editors,
pre-commit, and CI. Sections marked **[decision]** record a choice made here;
**[open]** marks something unresolved.

Companion documents:

- [`SPEC.md`](./SPEC.md) — the authoritative design (CLI, config, architecture).
- [`PRD.md`](./PRD.md) — product requirements, personas, milestones, metrics.
- [`RULES.md`](./RULES.md) — the full rule catalog.
- [`DIALECTS.md`](./DIALECTS.md) — the dialect registry and per-dialect notes.
- [`TEMPLATING.md`](./TEMPLATING.md) — how templated SQL is rendered and mapped.
- [`PERFORMANCE.md`](./PERFORMANCE.md) — budgets, benchmark harness, and corpus
  timings.

---

## 1. Integration surfaces

| Surface | Command | Consumed by |
|---|---|---|
| CLI | `rusq check`, `rusq format` | CI, `pre-commit`, `make`, task runners |
| Language server | `rusq server` | VS Code, Neovim, Helix, Emacs, Zed, JetBrains |
| Shell completions | `rusq generate-shell-completion` | bash, zsh, fish, PowerShell |
| CI action | `ppreeper/rusq@<ref>` | GitHub Actions, Forgejo Actions |

### 1.1 The parity guarantee

The editor and CI share **one engine**: the same templater, lexer, parser, rule
set, fix engine, and formatter. The LSP does not re-implement any of it — it is
a thin adapter that calls the same library the CLI calls. A finding shown in the
editor and the same finding reported by `rusq check` in CI are produced by the
same code path and carry the same code, span, and fix. **[decision]**

This matters most where configuration and templating interact: both surfaces
resolve `.rusq.toml` the same way, select the same rules, and map templated
output back to source positions identically.

---

## 2. Scaffolding a project

```console
$ rusq init                 # writes .rusq.toml in the current directory
$ rusq init models/ --force # elsewhere, overwriting
```

The starter config is valid immediately and matches the built-in defaults, so
you edit it only to *change* behaviour. `rusq config` lists every key and its
effect; `rusq config <KEY>` describes one.

---

## 3. CLI for CI and hooks

```console
$ rusq check .                          # lint; exit 1 on findings, 2 on error
$ rusq check --output-format sarif -o rusq.sarif .
$ rusq check --statistics .
$ rusq format --check .                 # exit 1 if anything would change
$ rusq check --add-ignore .             # write suppressions for existing findings
```

### 3.1 Exit codes

The ESLint/Prettier/Ruff contract (SPEC §2.4): **[decision]**

| Code | `check` | `format --check` |
|---|---|---|
| `0` | no findings (or all fixed) | nothing would change |
| `1` | findings | something would change |
| `2` | abnormal termination (bad config/dialect, IO) | same |

`--exit-zero` forces `0` on findings; `--exit-non-zero-on-fix` exits `1` when a
fix was applied. `NO_COLOR` and `--color auto|always|never` control colour;
machine-readable formats are never decorated.

### 3.2 Output formats

`concise`, `full`, `json`, `json-lines`, `github`, `gitlab`, `junit`, `sarif`,
`grouped`, `azure` (SPEC §11.1). `--output-file`/`RUSQ_OUTPUT_FILE` redirects the
report. The `github` format emits `::error file=…,line=…,col=…::CODE message`
annotations that Actions and Forgejo render inline; `sarif` feeds the security
dashboards; `junit` feeds most CI test tabs. **[decision]**

### 3.3 Common recipes

```console
# Fail CI on any finding, but keep the full report.
$ rusq check --output-format github .

# Verify formatting without writing.
$ rusq format --check --output-format github .

# Autofix safe issues and report what remains.
$ rusq check --fix --show-fixes .

# Enforce one dialect and templater in a repo-wide job.
$ rusq check --dialect snowflake --templater jinja models/
```

### 3.4 pre-commit

The repository ships hooks, so pin a revision and get everything with no local
scripting:

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/ppreeper/rusq
    rev: v0.1.2
    hooks:
      - id: rusq-check    # lint; non-zero exit on findings
      - id: rusq-format   # rewrite with the formatter
```

`rusq-check` runs `rusq check --force-exclude` and `rusq-format` runs
`rusq format --force-exclude`, so your `exclude`/`extend-exclude` patterns apply
to the files pre-commit hands over. Both use `language: golang`: pre-commit
builds the binary from source, so a Go toolchain is required but no pre-built
`rusq` is. `rusq-format` declares `require_serial: true` because it writes files
in place. **[decision]**

To use the binary already on your `PATH` instead:

```yaml
  - repo: local
    hooks:
      - id: rusq
        name: rusq
        entry: rusq check --force-exclude
        language: system
        files: \.(sql|dml|ddl)$
```

### 3.5 GitHub / Forgejo Action

```yaml
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ppreeper/rusq@v0.1.2
        with:
          args: check --output-format github .
```

The action downloads (or caches) the release binary for the runner's platform,
so there is no toolchain step. Pass `args: format --check .` to gate formatting
instead. The same composite action runs under Forgejo Actions. **[decision]**

### 3.6 Shell completions

`rusq generate-shell-completion <bash|zsh|fish|powershell>` prints a completion
script covering the subcommands, every flag, the output formats, the dialects,
the templaters, the colour modes, and rule selectors (`ALL`, categories,
families and rule codes).

```console
# bash
$ rusq generate-shell-completion bash > ~/.local/share/bash-completion/completions/rusq

# zsh (first fpath entry)
$ rusq generate-shell-completion zsh > "${fpath[1]}/_rusq"

# fish
$ rusq generate-shell-completion fish > ~/.config/fish/completions/rusq.fish

# PowerShell
$ rusq generate-shell-completion powershell >> $PROFILE
```

---

## 4. Language server

`rusq server` speaks LSP over **stdio** and provides: **[decision]**

| Capability | Backed by | Notes |
|---|---|---|
| `textDocument/publishDiagnostics` | `rusq check` | the same rules, on open and change |
| `textDocument/formatting` | `rusq format` | whole document as a single edit |
| `textDocument/rangeFormatting` | `rusq format --range` | formats the smallest enclosing element; the rest is untouched |
| `textDocument/codeAction` | the fix engine | a `quickfix` per fixable diagnostic, plus `source.fixAll.rusq` for safe fixes |

### 4.1 Document sync and configuration

The server is **full-sync** — each change sends the whole document — which keeps
the client adapter trivial and the results identical to reading the file from
disk. **[decision]**

Configuration is resolved **per file**, exactly as the CLI does it: the server
walks from the file's directory upward collecting `.rusq.toml`/`rusq.toml`, so
per-directory settings work with no server-side configuration. A client may pin
one file with `--config <path>` or disable discovery with `--isolated`.

```
rusq server [--config <FILE>] [--isolated] [--dialect <D>] [--templater <T>]
            [--stdio] [--clientProcessId <PID>]
```

`--stdio` and `--clientProcessId` are accepted for LSP-client compatibility.
Clients such as `vscode-languageclient` append them when launching an
`Executable` over stdio; the server always speaks stdio, so both are ignored.

`--dialect` and `--templater` set the fallback when a file's configuration does
not. Diagnostics are published against the document version that produced them;
stale results for an older version are dropped. **[decision]**

### 4.2 Templating and dialects in the editor

Because templated SQL is linted in rendered variants and mapped back to source
(TEMPLATING.md), the server reports diagnostics at the position a developer sees
in the file, not in the rendered output. **[decision]**

- Diagnostics from every variant are merged and emitted once, with provenance
  available in the `data` field for clients that surface it.
- A fix that would touch a templated slice (`{{ … }}`, `{% … %}`) is
  **display-only**: it appears in `--diff`/`full` but is not offered as a
  `quickfix`, so an editor never rewrites template logic. Code actions that
  would edit a templated slice are withheld. **[decision]**
- `JJ01` (`jinja.padding`) runs on the raw source under the jinja templater; the
  server surfaces it in the editor like any other finding.
- The dialect is inferred per file (extension, then config, then the server
  fallback). Version gating uses the resolved `target-version`; a construct
  valid in the file's target version is not flagged.

### 4.3 Suppressions and code actions

Inline `-- rusq:` / `-- noqa:` directives are honoured in the editor exactly as
in the CLI. In addition to `quickfix` and `source.fixAll.rusq`, the server
offers a **suppress** action that inserts the correct directive (`-- rusq: CODE`)
at the diagnostic's line. **[decision]**

### 4.4 Performance in the editor

Per-file lint latency is **< 5 ms** median for typical models (NFR-PERF-2), so
publish-on-change feels instant. The server reuses the same `.rusq_cache/`
result cache as the CLI, keyed by content and settings, so switching between an
editor run and a CI run stays warm. Cross-file index checks are published when
`[index].enabled = true`: the server builds the project index from the workspace
root, records each open buffer in it so unsaved edits are reflected, and drops
the index on save, close, configuration change, or watched-file change so the
next lint rebuilds it.

### 4.5 Starting the server

Any LSP client that can launch a process over stdio works:

```
command: rusq
args:    ["server"]
```

If the server fails to start (binary missing, bad `--config`), clients should
surface the stderr log. Server logging is controlled by the client's trace
setting; the `RUSQ_LOG` environment variable name and level values are
**[open]**.

---

## 5. Editor setup

### 5.1 Neovim (0.11+)

```lua
vim.lsp.config.rusq = {
  cmd = { "rusq", "server" },
  filetypes = { "sql" },
  root_markers = { ".rusq.toml", "rusq.toml", ".git" },
}
vim.lsp.enable("rusq")
```

Format on save, with the server as the default formatter:

```lua
vim.api.nvim_create_autocmd("BufWritePre", {
  pattern = "*.sql",
  callback = function()
    vim.lsp.buf.format({ name = "rusq", async = false })
  end,
})
```

On older Neovim, start it per buffer:

```lua
vim.api.nvim_create_autocmd("FileType", {
  pattern = "sql",
  callback = function()
    vim.lsp.start({
      name = "rusq",
      cmd = { "rusq", "server" },
      root_dir = vim.fn.getcwd(),
    })
  end,
})
```

### 5.2 Helix

```toml
# ~/.config/helix/languages.toml
[[language]]
name = "sql"
language-servers = ["rusq"]
formatter = { command = "rusq", args = ["format", "-"] }
auto-format = true

[language-server.rusq]
command = "rusq"
args = ["server"]
```

### 5.3 VS Code

Install the extension from the marketplace, or build the client that ships in
[`editors/vscode`](../editors/vscode) (see §6). It is a plain stdio client for
`rusq server`, so it uses whatever `rusq` binary you have installed. To make
`rusq` the default SQL formatter:

```json
"[sql]": { "editor.defaultFormatter": "ppreeper.rusq-sql" }
```

### 5.4 Other LSP clients

Emacs (`eglot`/`lsp-mode`), Zed, Sublime LSP, and JetBrains all launch
`rusq server` over stdio; register the command for the `sql` language and point
the client at the repo root so `.rusq.toml` discovery resolves. A dedicated
plugin is not required — the server is the integration point.

---

## 6. The VS Code client

A thin LSP client ships in [`editors/vscode`](../editors/vscode). It bundles no
linter logic; it launches `rusq server` and maps its diagnostics, edits, and
code actions into the VS Code API. **[decision]**

The client registers for VS Code's built-in `sql` language id plus the ids
popular dialect and templating extensions use — `jinja-sql`, `sql-bigquery`,
`dbt-sql`, `pgsql`, `mysql`, and `plsql` — for both `file` and `untitled`
documents. Dialect and templater are never chosen by the client; the server
infers them per file from `.rusq.toml` discovery. **[decision]**

### 6.1 Settings

| Setting | Default | Meaning |
|---|---|---|
| `rusq.binaryPath` | `""` | Path to the `rusq` binary. Empty looks next to the extension, then `rusq` on `PATH`. |
| `rusq.configPath` | `""` | Passed as `--config <path>`. Empty uses `.rusq.toml` discovery per file. |
| `rusq.trace.server` | `"off"` | `off` \| `messages` \| `verbose` — JSON-RPC tracing. |

### 6.2 Commands

- *rusq: Restart language server*
- *rusq: Show output channel*

### 6.3 Building from source

```console
$ cd editors/vscode
$ npm install
$ npm run build          # bundles src/ into dist/extension.js
$ npm run package        # rusq-sql-0.1.2.vsix
$ code --install-extension rusq-sql-0.1.2.vsix
```

Or open `editors/vscode` in VS Code and press <kbd>F5</kbd> to run it in an
Extension Development Host.

---

## 7. On-save workflows

### 7.1 Format on save

Prefer the language server (it formats the document in place through the editor's
undo stack). A CLI task is the fallback when no LSP client is configured:

```console
$ rusq format --stdin-filename "$FILE" - < "$FILE" > "$FILE.tmp" && mv "$FILE.tmp" "$FILE"
```

### 7.2 Lint on save

Run the CLI as a VS Code task and let the problems panel collect the output:

```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "rusq check",
      "type": "shell",
      "command": "rusq check ${relativeFile}",
      "problemMatcher": []
    }
  ]
}
```

`rusq check --output-format github` emits `::error file=…,line=…::CODE message`
lines, which CI and many editors annotate directly; `sarif`, `gitlab`, and
`junit` are available for the usual dashboards.

### 7.3 Troubleshooting

| Symptom | Likely cause |
|---|---|
| No diagnostics in the editor | Server not running; check *rusq: Show output channel*. |
| Different findings in editor vs CI | A different binary or config on `PATH`; pin `rusq.binaryPath` and verify `.rusq.toml` discovery from the repo root. |
| Formatting rewrites template logic | It should not — fixes touching a templated slice are display-only (TEMPLATING.md); report it as a bug if it happens. |
| Slow first run in a large repo | Cold cache; the warm run is `< 1 s` (NFR-PERF-1). |

---

## 8. Open questions

- **[open]** Exact `RUSQ_LOG` variable name and level values for server logging
  (§4.5).
- **[open]** Whether templated extensions (`.sql.j2`, `.sql.jinja`,
  `.sql.jinja2`) are added to the default `sql-file-exts`, or inferred only when
  `--templater jinja` is set — this affects the pre-commit `files:` pattern and
  editor `filetypes` (§3.4).
- **[open]** Whether the server should offer a `workspace/`-level command to run
  `rusq check` across the project and populate the problems panel from one run.
- **[open]** Whether a first-party plugin is worth shipping for Neovim beyond the
  `vim.lsp.config` snippet.

---

*This document is a living part of the design set. When integration work refines
a claim, the claim moves here first and the code follows.*
