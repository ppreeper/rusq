# rusq — Specification

> An extremely fast SQL linter and formatter, written in Go, in the spirit of
> [Ruff](https://docs.astral.sh/ruff/), with first-class support for the many
> dialects and templating layers real SQL is written in.

Status: **draft v0.1** — this document is the authoritative design. It is
intentionally opinionated. Sections marked **[decision]** record a choice made
here; sections marked **[open]** are unresolved and need review before
implementation.

Companion documents:

- [`PRD.md`](./PRD.md) — the product requirements: users, scope, milestones,
  success metrics.
- [`TEMPLATING.md`](./TEMPLATING.md) — how templated SQL is rendered, traced, and
  mapped back to source.
- [`RULES.md`](./RULES.md) — the full rule catalog: codes, names, severity,
  fixability, category, default state, and dialect applicability.
- [`DIALECTS.md`](./DIALECTS.md) — the dialect registry, inheritance graph,
  quoting/casing model, `target-version` gating, and per-dialect notes.
- [`EDITOR.md`](./EDITOR.md) — editor (LSP), CI, and pre-commit integration.
- [`PERFORMANCE.md`](./PERFORMANCE.md) — budgets, benchmark harness, and corpus
  timings.

---

## 1. Overview

`rusq` is a single, self-contained Go binary exposing two tools behind one
interface:

- **`rusq check`** — a linter. It parses SQL into a lossless concrete tree with
  a typed AST projection for structural rules, runs a large set of rules
  organized by family, and reports diagnostics with byte-exact spans. Many
  diagnostics carry **safe** or **unsafe** automatic fixes.
- **`rusq format`** — an opinionated formatter. It reprints SQL from a lossless
  parse tree using a width-aware document model. It is designed to be
  **idempotent** and to **never introduce new lint errors**.

Around those two cores sit the ergonomics developers expect from a modern
linter:

- A single `.rusq.toml` configuration file with hierarchical discovery.
- Flake8-style **rule codes** (`AL01`, `CP01`, `LT05`, …) selectable by code,
  prefix, group, or category, with **migration-compatible codes** carried over
  from the mature Python reference (§7.1).
- Suppression directives written as SQL comments (`-- rusq: …`).
- `--fix`, a safe/unsafe fix taxonomy, `--diff`, stdin (`-`), caching, watch mode.
- A stable exit-code contract (`0` clean, `1` findings, `2` abnormal).
- Machine-readable output (JSON, JSON-lines, SARIF, GitHub, GitLab, JUnit, Azure).

### 1.1 Goals

1. **Correctness first.** Catch the SQL mistakes that actually change results,
   silently drop rows, or break a migration, before the warehouse or CI does.
2. **Speed.** Fast enough to run on a whole dbt/SQLMesh project on every save.
   Target: parse + lint + format a 20k-file project in well under a few seconds,
   single-threaded per core and scaling near-linearly with cores. This is the
   defining reason the project exists: the mature Python reference is the
   coverage gold standard, but it is slow and needs an interpreter; the
   project's own reason to exist is *the same breadth at native speed*.
3. **One binary, no runtime.** No Python, no Node, no plugins. `go build` and
   ship. Optional Python-touching templaters (§9) degrade gracefully rather than
   becoming hard dependencies.
4. **Breadth of coverage is the product.** The whole point is *how much SQL it
   understands*: many dialects, many templating layers, and a large rule
   catalog — not a toy linter for `SELECT`.
5. **Opinionated, batteries-included defaults.** Zero config catches real bugs;
   deeper style guidance is one `extend-select` away.
6. **Dialect-aware, not dialect-locked.** Generic ANSI quality works everywhere;
   dialect-specific rules and grammars activate for the dialects in use.

### 1.2 Non-goals (v1)

- **Not** a database driver, executor, or query planner. `rusq` never connects
  to a database and never returns result sets.
- **Not** a schema validator or type checker. Semantic/ast-model checks against
  real catalog metadata are out of scope for v1 (§8.3).
- **Not** a full SQL compiler for every templating engine. Jinja is supported
  natively; full dbt/SQLMesh compilation that requires the Python toolchain is
  **best-effort and optional** (§9), not a v1 blocker.
- **Not** a script migration tool. `--fix` applies style/correctness edits; it
  does not rewrite schemas or data.
- **Not** a general-purpose orchestrator. It lints and formats files; it does
  not run models, tests, or DAGs.

### 1.3 Name

Binary and config stem: `rusq`. Module path: `github.com/ppreeper/rusq`.
Config files: `.rusq.toml` / `rusq.toml`. Cache dir: `.rusq_cache/`.
Suppression prefix: `rusq:` (with `noqa:` accepted as an alias for
compatibility with SQL tooling conventions). **[decision]**

---

## 2. Command-line interface

### 2.1 Subcommands

```
rusq check      [PATH]...        # lint (default when no subcommand)
rusq format     [PATH]...        # format
rusq parse      [PATH]...        # dump the parse tree (debug) — json|yaml|pretty|none
rusq rule       [CODE|SELECTOR]  # explain a rule, or list rules
rusq config     [KEY]            # list or describe configuration keys
rusq dialects                    # list supported dialects and coverage
rusq templaters                  # list supported templaters and options
rusq init                        # write a starter .rusq.toml
rusq clean                       # clear cache(s)
rusq server                      # language server (LSP over stdio)
rusq version
rusq help
rusq generate-shell-completion <bash|zsh|fish|powershell>
```

Every subcommand accepts `-h`/`--help` and prints a usage screen (exit 0) that
documents its flags, exit codes and examples; `rusq help` lists the commands.
`rusq generate-shell-completion` prints a completion script covering the
subcommands, every flag, the output formats, the dialects, the colour modes, and
rule selectors (`ALL`, categories, families, and rule codes).

When invoked with no subcommand, `rusq` behaves as `rusq check`. **[decision]**

### 2.2 `rusq check` flags

| Flag | Meaning |
|---|---|
| `--fix` / `--no-fix` | apply fixes (safe by default) |
| `--unsafe-fixes` / `--no-unsafe-fixes` | include unsafe fixes |
| `--show-fixes` | summarize fixes applied |
| `--diff` | print a unified diff instead of writing (implies `--fix-only`) |
| `--fix-only` | apply fixes, do not report remaining diagnostics |
| `--select <SEL>` | comma list of codes/prefixes/groups/categories/`ALL` |
| `--ignore <SEL>` | remove from the selected set |
| `--extend-select <SEL>` | add to the selected set |
| `--extend-ignore <SEL>` | add to the ignored set |
| `--per-file-ignores <GLOB:CODE[,CODE...]>` | per-glob ignores from the CLI; repeat for several globs |
| `--fixable <SEL>` / `--unfixable <SEL>` | limit which rules may fix; `--unfixable` wins |
| `--warnings <SEL>` | downgrade rules to warnings so they do not fail the run |
| `--add-ignore [REASON]` / `--add-noqa [REASON]` | author suppressions for existing findings |
| `--ignore-suppressions` | ignore all `rusq:` directives (audit mode) |
| `--disable-noqa` / `--disable-noqa-except <SEL>` | global/selective suppression override |
| `--dialect <D>` | dialect: `ansi`, `postgres`, `bigquery`, `snowflake`, … (§8.1) |
| `--templater <T>` | templater: `raw`, `placeholder`, `jinja`, `python`, `dbt`, `sqlmesh` (§9) |
| `--output-format <FMT>` | `concise`, `full`, `json`, `json-lines`, `github`, `gitlab`, `junit`, `sarif`, `grouped`, `azure` |
| `-o, --output-file <FILE>` | write report to file (env `RUSQ_OUTPUT_FILE`) |
| `--statistics` | per-rule counts |
| `--exit-zero` | always exit 0 on findings |
| `--exit-non-zero-on-fix` | exit 1 if anything was fixed |
| `--parsing-errors` | fail (exit 2) on parse errors instead of reporting them |
| `--exclude` / `--extend-exclude` / `--force-exclude` | file selection; `--force-exclude` also applies `exclude` to paths given explicitly |
| `--respect-gitignore` / `--no-respect-gitignore` | gitignore handling |
| `-n, --no-cache` / `--cache-dir <DIR>` | cache control (env `RUSQ_NO_CACHE`, `RUSQ_CACHE_DIR`) |
| `--workers <N>` | parallel worker limit (default: number of CPUs) |
| `-w, --watch` | re-run on file changes |
| `--color <auto\|always\|never>` | colour control (also `--no-color`) |
| `--stdin-filename <NAME>` | logical path when reading from `-` |
| `--show-files` / `--show-settings` | print the selected files, or the effective settings per file |
| `--preview` | gate for experimental rules: never enabled by prefix/category/`ALL`, and selecting one by code without it is an error |
| `--isolated` | ignore all config files |
| `--config <FILE\|KEY=VALUE>` | override config |
| `-v/-q/-s` | verbosity: `-v` writes a timing summary to stderr, `-q` hides the fix summary, `-s` silences stdout (exit code unchanged) |

### 2.3 `rusq format` flags

`--check`, `--diff`, `--line-length`, `--indent-width`, `--indent-unit
<space|tab>`, `--range <start_line>:<col>-<end_line>:<col>` (range/selection
formatting), `--stdin-filename`, `--exit-non-zero-on-format`, plus the shared
file-selection, cache, config, dialect, templater, and global flags.
**[decision]**

`--check` and `--diff` accept `--output-format` and `-o/--output-file` too, so a
CI job can consume the formatter verdict (e.g. `rusq format --check
--output-format json`) from the same ten formats the linter uses.

Both `rusq check --diff` and `rusq format --diff` print a real unified diff —
`--- path` / `+++ path` headers, `@@ -l,c +l,c @@` hunks with three lines of
context, and `\ No newline at end of file` where it applies — instead of
writing. `check --diff` implies `--fix`: the fixes are applied in memory and
shown as a patch.

`--range start_line:col-end_line:col` (1-based lines and rune columns) formats
only the smallest element enclosing the selection and leaves every other byte
untouched; a bare line means the whole line. `--check` and `--diff` compose with
it, and the language server exposes the same engine as
`textDocument/rangeFormatting`.

### 2.4 Exit codes

Mirrors Ruff / ESLint / Prettier. **[decision]**

| Code | `check` | `format` | `format --check` |
|---|---|---|---|
| `0` | no findings (or all fixed) | success | nothing would change |
| `1` | findings | — | something would change |
| `2` | abnormal termination (bad config/dialect, parse-error-as-fatal, IO) | same | same |

### 2.5 stdin contract

`-` reads from stdin; `--stdin-filename <NAME>` supplies the logical path so
per-file-ignores, dialect inference, and editor-relative reporting work. When
reading stdin the tool never writes files: fixes are returned to stdout (or as a
diff). **[decision]**

---

## 3. Configuration

### 3.1 Discovery

`rusq` walks from the file's directory upward to the filesystem root, collecting
`rusq.toml` and `.rusq.toml` files. **Closest wins**, per key. An outer file may
be extended explicitly with `extend = "path"`. Precedence, highest first:
**[decision]**

1. CLI flags (`--config KEY=VALUE`, `--select`, `--dialect`, …)
2. `rusq.toml` / `.rusq.toml` in a nearer directory
3. `rusq.toml` / `.rusq.toml` in an outer directory (via `extend` or discovery)
4. user config (`$XDG_CONFIG_HOME/rusq/rusq.toml`, else `~/.config/rusq/rusq.toml`)
5. built-in defaults

`--isolated` ignores all config files. `--config <FILE>` pins one file.

### 3.2 Schema (TOML)

```toml
dialect = "postgres"          # global default; may be set per-directory
templater = "jinja"
line-length = 88
target-version = "13"         # interpreted per dialect (§8.2)
sql-file-exts = [".sql", ".dml", ".ddl"]
render-variant-limit = 5
max-parse-depth = 255
max-parse-nodes = 100000
encoding = "utf-8"

[lint]
select = ["core"]             # default set
extend-select = []
ignore = []
warnings = []                 # downgraded to warnings (do not fail the run)
fixable = ["ALL"]             # which rules may apply fixes
unfixable = []                # removed from the fixable set (wins)
exclude-rules = []
per-file-ignores = { "migrations/**" = ["LT05", "CP01"] }
preview = false

[dialects]
# per-dialect overrides; keys fall back to the global settings
postgres = { target-version = "16" }
bigquery = { capitalisation-policy = "upper" }

[templaters]
# templater-specific configuration (§9)
jinja = { apply-dbt-builtins = true, load-macros-from-path = ["macros"] }
placeholder = { param-style = "colon" }

[rules]
# rule-specific options; keys are the rule's config keys (§7.2)
"capitalisation.keywords" = { capitalisation-policy = "upper" }
"layout.operators" = { line-position = "trailing" }
"convention.count_rows" = { prefer-count = "count_star" }

[format]
indent-unit = "space"
indent-width = 4
line-position = "trailing"    # default for trailing/leading comma+operator families
keyword-line-position = "leading"
# …formatter options (§10.4)

[index]
# opt-in cross-file checks (§8.3)
enabled = false
```

> `[dialects]` and `[templaters]` are plural on purpose: TOML cannot have both a
> `dialect = "…"` scalar and a `[dialect]` table (likewise for `templater`), so
> the per-dialect and templater-option tables use distinct names. **[decision]**

An INI compatibility reader (`.rusq` / `setup.cfg`-style keys) is **[open]** and
not planned for v1; TOML is the only config format. **[decision]**

### 3.3 Selector grammar

A selector names a rule by **code** (`LT05`), **prefix** (`LT`), **group**
(`layout`), **category** (`style`), or `ALL`. Comma-separated lists and repeated
flags are accepted. Precedence, lowest to highest: **[decision]**

```
ALL  <  category  <  group  <  prefix  <  code
```

Selection is resolved as: `(select ∪ extend-select) − (ignore ∪ extend-ignore)`,
then per-file-ignores, then `--fixable`/`--unfixable` restrict fixing. Within a
tie, CLI beats config beats an extended config. Deprecated aliases (the legacy
`L003`-style codes shipped by earlier tools) are accepted and resolved to their
new code, emitting a one-time deprecation notice. **[decision]**

### 3.4 `per-file-ignores`

A map of glob → selector list. Globs are matched against the path relative to
the config file, using `doublestar` semantics (`**` matches across separators).
`--per-file-ignores` on the CLI adds to (never replaces) the config map.
**[decision]**

---

## 4. File discovery & kinds

Discovery is gitignore-aware: `include`/`exclude` globs from config, plus
`--exclude`, `--extend-exclude`, `--force-exclude`, and `--show-files`. It reads
`.ignore`, `.gitignore`, `.git/info/exclude`, and the user-level gitignore.
`--force-exclude` also applies `exclude` to paths passed explicitly. **[decision]**

| Kind | Detection | Handling |
|---|---|---|
| `sql` | extension in `sql-file-exts` | linted + formatted |
| `ignored` | matches exclude/ignore | skipped |
| `unreadable` | IO/permission error | reported, non-fatal |

Files with unsupported extensions passed explicitly are reported, not silently
skipped. `--show-files` prints the resolved set and the reason each file was
included or excluded. **[decision]**

---

## 5. Diagnostics model

A diagnostic is:

```go
type Diagnostic struct {
    Code     string      // "LT05"
    Name     string      // "layout.long_lines"
    Severity Severity    // Error | Warning | Hint
    Span     Range       // byte offsets (primary)
    Message  string      // one line
    Help     string      // optional, shown in `full`
    Fix      *Fix        // optional
    Fixability Fixability // Safe | Unsafe | Display | None
    Provenance []string  // e.g. which templated variant produced it
    Related  []Related   // optional secondary spans
}

type Fix struct{ Edits []Edit } // ordered, non-overlapping by construction
type Edit struct{ Span Range; Replacement string }
```

Rendering:

- `concise`: `path:line:col: CODE message`
- `full`: multi-line, includes name, help, and a source excerpt with a caret span
- machine formats (§11) map fields directly and never include colour

Severity is `error` by default for `core` rules; `warning` for `style`/`pedantic`
and any rule listed in `warnings`. Warnings are printed but do not by themselves
fail the run. **[decision]**

---

## 6. Suppression directives

Suppressions are SQL comments. Both `-- rusq:` and `-- noqa:` are accepted.
**[decision]**

```sql
SELECT * FROM t;                 -- rusq: LT05
SELECT * FROM t;                 -- noqa: LT05, CP01
-- rusq: disable=AL02,ST03
SELECT ...;
-- rusq: enable=all
-- rusq: file-ignore=RF01
-- rusq: fmt off
...                            -- rusq: fmt on
```

Directives: `ignore` (line), `disable`/`enable` (range), `file-ignore` (whole
file), `fmt off`/`fmt on` (formatter), and `skip` (skip the next statement).
`--add-ignore`/`--add-noqa` author suppressions for existing findings;
`--ignore-suppressions` ignores all directives for a clean audit.

Meta rules police bad suppressions (§7.1):

| Code | Name | Checks |
|---|---|---|
| `RUS101` | `rusq.invalid` | a directive names an unknown rule |
| `RUS102` | `rusq.unused` | a directive silences nothing |
| `RUS103` | `rusq.deprecated` | a directive references a deprecated code |
| `RUS104` | `rusq.parse_error` | a directive cannot be applied (unparseable region) |

---

## 7. Rule system

### 7.1 Families

Codes are `PREFIX` + digits. The **core families reuse the codes established by
the mature Python SQL linter** so a repository migrating to `rusq` keeps its
`--select`/`--ignore` lists and inline `noqa` directives working unchanged.
**[decision]**

| Prefix | Family | Domain | Origin |
|---|---|---|---|
| `AL` | `aliasing` | generic | baseline |
| `AM` | `ambiguous` | generic | baseline |
| `CP` | `capitalisation` | generic | baseline |
| `CV` | `convention` | generic | baseline |
| `JJ` | `jinja` | templater (jinja) | baseline |
| `LT` | `layout` | generic | baseline |
| `RF` | `references` | generic | baseline |
| `ST` | `structure` | generic | baseline |
| `PG` | `postgres` | dialect | baseline (dialect) |
| `OR` | `oracle` | dialect | baseline (dialect) |
| `TQ` | `tsql` | dialect | baseline (dialect) |
| `MY` | `mysql` | dialect | extension |
| `BQ` | `bigquery` | dialect | extension |
| `SF` | `snowflake` | dialect | extension |
| `RS` | `redshift` | dialect | extension |
| `CH` | `clickhouse` | dialect | extension |
| `TD` | `teradata` | dialect | extension |
| `TR` | `trino` | dialect | extension |
| `DG` | `databricks` | dialect | extension |
| `SP` | `sparksql` | dialect | extension |
| `PL` | `placeholder` | templater (placeholder) | extension |
| `RUS` | `rusq` | meta | native |

The **`Origin`** column records where a family's codes come from: `baseline` and
`baseline (dialect)` families reuse codes so a migrated repository keeps its
selectors working; `extension`, `templater`, and `native` families are added by
`rusq`. Whether a rule is **on by default is decided per rule by its category**
(§7.3 — `correctness`, `suspicious`, and `security` are on; everything else is
opt-in), *not* by family. The default set is therefore deliberately small and
Ruff-like; the exact on/off state of every rule is tabulated in
[`RULES.md`](./RULES.md). **[decision]**

Core families use **two-digit** codes (`LT05`); the native meta family uses
**three-digit** codes (`RUS101`) so the two namespaces never collide and the
meta namespace has room to grow. **[decision]**

> The `JJ` (jinja) family only activates when the jinja templater is in use; the
> `PL` family only when the placeholder templater is in use. Selecting a
> templater-specific rule under the wrong templater is a no-op, not an error.
> **[decision]**

### 7.2 Rule metadata

Every rule declares:

```go
type RuleMeta struct {
    Code       string   // "LT05"
    Name       string   // "layout.long_lines"
    Family     string   // "layout"
    Category   Category // correctness|suspicious|style|security|pedantic|restriction|formatting
    Severity   Severity // error|warning|hint
    Source     LintSource
    Fixable    Fixability // safe|unsafe|display|none
    Default    bool       // on in the default set
    Stable     bool       // stable vs preview
    Dialects   []string   // empty = all dialects
    ConfigKeys []ConfigKey
}

type LintSource int
const (
    Token LintSource = iota // cheapest: single lexical pass
    Lines                   // logical/physical line view
    Tree                    // typed AST projection of the CST (§12.2)
    File                    // whole-file (e.g. trailing newline)
    Index                   // cross-file (opt-in)
)
```

`Source` drives the cheapest-first pass schedule (§12.4): a file is lexed once,
line-scanned rules run, then tree rules run only on files that need them.

### 7.3 Categories

`correctness` (default on), `suspicious` (default on), `style` (off; opt-in),
`security` (default on), `pedantic` (off), `restriction` (off), `formatting`
(off; overlaps the formatter, opt-in for lint-only workflows). **[decision]**

### 7.4 Linter / formatter separation

A hard invariant: **the formatter must never introduce a `check` finding.** The
`formatting`-category rules mirror formatter behaviour so a lint-only user can
enforce the same style without running `format`; when both run, the formatter
wins and the lint rule does not double-report. Style rules are off by default so
`check` stays focused on correctness. **[decision]**

### 7.5 Fix safety

`safe` fixes preserve semantics and are applied by `--fix`. `unsafe` fixes may
change behaviour (e.g. reordering columns, rewriting `COUNT(*)`) and require
`--unsafe-fixes`. `display` fixes are shown in `--diff`/`full` but never
applied automatically. The engine sorts edits by start offset then by
stability, applies them greedily and non-overlapping within isolation groups,
iterates to `maxIterations = 100`, and **reverts any pass that introduces a
syntax error**. **[decision]**

### 7.6 Catalog

The full catalog (every code, name, severity, fixability, default state, and
dialect applicability) lives in [`RULES.md`](./RULES.md). v1 targets **full
coverage of the mature Python SQL linter's 79-rule catalog** (the compatibility
baseline) **plus the rusq-native meta and broadened dialect families**, for a
target of **≥ 120 catalogued rules** (§PRD FR-RULE). The catalog is versioned
with the binary; `rusq rule <CODE>` prints a rule's metadata and examples.

---

## 8. Dialect and version support

### 8.1 Dialects (27, extensible)

`ansi` (base grammar, the default), `athena`, `bigquery`, `clickhouse`,
`databricks`, `db2`, `doris`, `duckdb`, `exasol`, `flink`, `greenplum`, `hive`,
`impala`, `mariadb`, `materialize`, `mysql`, `oracle`, `postgres`, `redshift`,
`snowflake`, `sparksql`, `sqlite`, `starrocks`, `teradata`, `trino`, `tsql`,
`vertica`. **[decision]**

`redshift`, `greenplum`, `materialize`, `doris`, and `starrocks` inherit from
their nearest ancestor (`postgres`/`mysql`) and override only what differs. All
dialects inherit the ANSI base grammar and override specific segments.
Dialect selects: default keyword casing, string/identifier quote characters, the
set of valid segment types, and which rules are skipped (e.g. a rule meaningless
on a given dialect is skipped, not errored). Dialects are registered lazily and
never imported directly by the core. The full registry, inheritance graph,
quoting/casing model, and per-dialect notes live in [`DIALECTS.md`](./DIALECTS.md).

### 8.2 Version gating

`target-version` is interpreted **per dialect** (e.g. `postgres = "16"`,
`bigquery = "2021"`, `snowflake` uses feature flags rather than a number where
appropriate). Rules declare the versions in which a construct is valid and
only run when the target version warrants them. Absent a target version, only
version-independent rules run. **[decision]** Per-dialect value shapes and gate
examples are tabulated in [`DIALECTS.md`](./DIALECTS.md) §6.

### 8.3 Cross-file index (opt-in)

`[index].enabled = true` builds a project index (table/view/CTE definitions and
references) persisted to `.rusq_cache/index.json` and reused until a file
changes. Each file is read, lexed, parsed and projected once into a small set of
facts (definitions with spans, references with spans) plus a BLAKE2b-256 content
hash; a file whose hash is unchanged is not re-parsed. The index is keyed also by
dialect, so changing `dialect` discards it.

It powers the cross-file rules: `ST03` (`structure.unused_cte`) stops reporting a
CTE that another file references; `ST13` (`structure.duplicate_definition`)
reports a `table` or `view` defined in more than one file; `ST14`
(`structure.unused_definition`) reports a view no file in the project
references; and `RF08` (`references.unknown_object`) reports a `ref()`/`source()`
target that no file defines. Cross-file analysis is opt-in: with the index off,
`ctx.Index` is nil and those rules stay silent. It is skipped for stdin and for
single-file runs where it would add nothing.

The language server builds the same index from the workspace root and validates
the project, so cross-file findings appear in the editor (EDITOR §4.4). Each open
buffer is written into the index, so unsaved edits are visible to the cross-file
rules, and the index is discarded on save, close, configuration change, or
watched-file change.

Semantic checks against a *real* database catalog (column types, constraints)
remain out of scope for v1 (§1.2). **[decision]**

---

## 9. Templating

SQL in the wild is templated. The templater stage runs **before** lexing and
produces one or more render variants; diagnostics found in a variant are
reported against the **source** position, with provenance recording which
variant produced them. The full design — source slices, output tracing, variant
enumeration, and the substitution fallback — is in
[`TEMPLATING.md`](./TEMPLATING.md). **[decision]**

| Templater | Status | Notes |
|---|---|---|
| `raw` | v1 | pass-through; the default |
| `placeholder` | v1 | param styles: `colon`, `colon_nospaces`, `numeric_colon`, `pyformat`, `dollar`, `dollar_surround`, `question_mark`, `numeric_dollar`, `percent`, `ampersand`, `apache_camel`, `at`; configurable `param-style`, `param-regex`, sample values |
| `jinja` | v1 | native Go Jinja engine (`pongo2`, see [`TEMPLATING.md`](./TEMPLATING.md) §9): `apply-dbt-builtins`, `load-macros-from-path`, `exclude-macros-from-path`, `loader-search-path`, `library-path`, `ignore-templating`, plus a `[templaters.jinja.context]` table of variables |
| `python` | v1 (best-effort) | f-string/`.format()` and a `[templaters.python.context]` table; no Python runtime |
| `dbt` | v1.1 (optional) | full compilation shells out to a Python `dbt-core` install; without it, falls back to jinja and reports reduced coverage |
| `sqlmesh` | v1.1 (optional) | same optional-Python model |

The `jinja` templater has two modes. **Render mode** (the default) executes the
template with the engine, traces the output back to source with boundary
markers, and enumerates variants for untaken branches. **Substitution mode**
(`ignore-templating`) executes nothing: it replaces `{{ … }}` with placeholders
and treats `{% if %}` / `{% else %}` arms as concatenated so every branch is
linted. Substitution mode is the fallback whenever a template cannot be
rendered, so templating never aborts a run. **[decision]**

Multiple render variants (up to `render-variant-limit`, default 5) are combined;
a diagnostic is emitted once even if it appears in several variants, with
provenance listing them. `JJ01` (`jinja.padding`, single spaces inside `{{ }}`
and `{% %}`) runs on the raw source only under the jinja templater; every other
rule sees rendered variants. Fixes that would touch a templated slice are
downgraded to display-only. **[decision]**

The optional-Python templaters are a deliberate design point: the tool's *core*
never requires Python, but users already running dbt/SQLMesh can opt into full
compilation when they want maximum fidelity. **[open]** — whether v1 ships the
optional-Python bridge or defers it entirely to v1.1 (see
[`TEMPLATING.md`](./TEMPLATING.md) §12–13).

---

## 10. Formatter

### 10.1 Pipeline

```
source bytes
  → lossless lexer (tokens incl. comments/whitespace)
  → error-recovering parser (CST preserving trivia)
  → formatter IR (document model)
  → width-aware printer (Wadler/Prettier-style)
  → output bytes
```

### 10.2 Document IR

Prettier/Ruff-style combinators: `Text`, `Line`, `SoftLine`, `HardLine`,
`Group`, `Indent`, `IfBreak`, `Fill`, `LineSuffix`, `Dedent`. Groups flatten to
one line when they fit within `line-length`, otherwise break. **[decision]**

### 10.3 Formatter rules

The formatter owns layout: indentation, comma placement, operator placement,
keyword placement, blank-line normalization, CTE bracketing, `SELECT` target
layout, function-call spacing, and trailing newline. It does **not** reorder
columns, rename identifiers, or change semantics. **[decision]**

### 10.4 Options

`indent-unit` (`space`|`tab`), `indent-width`, `line-length` (default 88),
`line-position` (`trailing`|`leading`) for comma/operator families,
`keyword-line-position`, `single-target-policy` (`same_line`|`new_line`),
`comma-style`, `align-following`, `newline-at-eof`, `max-blank-lines`,
`cte-bracket-newline`, and per-segment-type overrides. **[decision]**

Options honoured by the current implementation: `indent-unit`, `indent-width`,
`line-length`, `line-position` (operators and boolean `AND`/`OR`; `trailing`
ends the line, `leading` opens it), `keyword-line-position` (`leading` keeps a
clause keyword at the start of its line; `trailing` leaves it at the end of the
previous line and starts the body on a new line), `single-target-policy`,
`comma-style` (`trailing`|`leading`, falling back to `line-position` when
unset), `align-following` (continuation rows line up under the first item
instead of the block indent), `newline-at-eof`, `max-blank-lines`, and
`cte-bracket-newline`. **[decision]**

Not yet applied: nothing in this list. Per-segment-type overrides are honoured for
`comma`, `operator`, and `keyword` (`[format.type.<segment>] line-position = …`),
which take precedence over the corresponding global option. **[decision]**

### 10.5 Safety & idempotency

`Format(Format(x)) == Format(x)` for all inputs (property-tested). Formatting a
file with parse errors leaves the unparseable region byte-identical and formats
only what parsed. `--range` formats only the smallest enclosing element.
Formatting never introduces a `check` finding (§7.4). **[decision]**

---

## 11. Output, caching, and watch

### 11.1 Output formats

`concise`, `full`, `json`, `json-lines`, `github`, `gitlab`, `junit`, `sarif`,
`grouped`, `azure`. Machine formats are never colourised and are stable across
patch releases. `--output-file` (env `RUSQ_OUTPUT_FILE`) redirects the report.
**[decision]**

### 11.2 Caching

Results are cached under `.rusq_cache/` keyed by a content hash (BLAKE2b-256) of
the file bytes **plus** a settings hash (dialect, templater, select/ignore,
rule options, tool version). A change to either invalidates. `--no-cache` and
`rusq clean` clear it. The cross-file index (§8.3) is stored separately and
reused until a contributing module changes. **[decision]**

### 11.3 Watch

`-w/--watch` re-discovers the tree on an interval, fingerprints every selected
file by size and mtime, and re-lints the whole set when anything changes; it
polls every 250 ms. It re-resolves settings each cycle, so edits to `.rusq.toml`
are picked up without a restart, and it clears the screen between runs only when
stdout is a terminal. Ctrl-C stops it and exits 0. `--watch` cannot be combined
with stdin. Respects `NO_COLOR`, `FORCE_COLOR`, `CLICOLOR`, `CLICOLOR_FORCE`,
and `--color auto|always|never`. **[decision]**

---

## 12. Architecture

### 12.0 Design stance

Deep modules with narrow interfaces; the core never imports the CLI. A strict
import layering (mirroring the discipline that makes the mature Python reference
maintainable): `syntax`/`tree`/`dialect` are leaf packages; `linter`, `rules`,
`fix`, `formatter` depend on them; `cli`/`output`/`lsp` depend on everything and
are depended on by nothing. **[decision]**

### 12.1 Directory layout

```
cmd/rusq/                 # main
internal/cli/             # cobra commands, flag plumbing
internal/config/          # TOML load, discovery, extend, precedence
internal/settings/        # resolved effective settings per file
internal/source/          # Text, Locator, byte-offset Range
internal/lexer/           # lossless lexer
internal/parser/          # error-recovering CST parser
internal/tree/            # lossless CST types, traversal, visitors
internal/ast/             # typed AST projection over the CST (rule-facing)
internal/dialect/         # ansi base + per-dialect overrides, lazy registry
internal/templater/       # raw, placeholder, jinja, python, (dbt, sqlmesh)
internal/index/           # opt-in cross-file index
internal/diagnostic/      # Diagnostic, Fix, Edit, rendering
internal/linter/          # pass scheduling, fix engine
internal/rules/           # one package per family; rule registry
internal/fix/             # edit application, isolation groups, revert
internal/formatter/       # document IR + printer
internal/output/          # concise/full/json/sarif/github/… renderers
internal/walk/            # gitignore-aware discovery
internal/cache/           # BLAKE2b-256 result cache
internal/lsp/             # language server
internal/report/          # statistics, summaries
internal/version/         # version + commit stamping
internal/linttest/        # lint test harness (AssertDiagnostics, AssertFix)
internal/formatest/       # formatter test harness (AssertFormat, idempotency)
```

### 12.2 Tree representation: lossless CST + typed AST

Parsing yields **two coordinated views from a single pass** — never two parsers:

- **Lossless CST (canonical).** The parser emits an error-recovering concrete
  syntax tree in which *every* token — whitespace, newlines and comments
  (trivia) included — is a node. This is the source of truth: it satisfies the
  round-trip property `Unparse(Parse(src)) == src` (§12.5) and is what the
  formatter, the fix engine and range formatting read.
- **Typed AST (projection).** `internal/ast` exposes a typed, trivia-free view
  over that same tree — typed wrappers/accessors keyed by node kind (`Select`,
  `Join`, `ColumnRef`, `Cte`, …) — so rule authors pattern-match over structure
  rather than walking a generic tree by hand. It is derived from the CST on
  demand; it is **not** a second parse and owns no independent grammar.

Why both: an AST alone is deliberately lossy (trivia dropped, redundant
parentheses normalised) — fine for semantic rules, fatal for a formatter and for
byte-exact fixes. A CST alone is faithful but verbose to write rules against.
Keeping the CST canonical and the AST a *view* gives rule ergonomics without
forfeiting fidelity, and avoids the drift a parallel AST grammar would introduce
across ~27 dialects.

The typed AST is the shape of the `Tree` lint tier (§12.4): families whose logic
is structural (`AL*`, `AM*`, `RF*`, `ST*`, `CV02/05/08/11/12`) consume the typed
view, while lexical and layout rules stay on the token and line tiers and never
pay for it. **[decision]**

### 12.3 Core interfaces

```go
type Templater interface {
    Render(src source.Text, s Settings) ([]Variant, []SyntaxError)
}

type Lexer interface { Lex(src source.Text, d *dialect.Dialect) ([]Token, []SyntaxError) }

type Parser interface {
    Parse(tokens []Token, d *dialect.Dialect) (*tree.Tree, []SyntaxError)
    Unparse(t *tree.Tree) []byte // must round-trip byte-identical
}

type Rule interface {
    Meta() RuleMeta
    Check(*lint.Context) []diagnostic.Diagnostic
}

type Formatter interface {
    Format(t *tree.Tree, s Settings) []byte
}
```

### 12.4 Pass schedule (cheapest first)

1. **Lex** once.
2. **`Token`-source rules** run on the token stream.
3. **`Lines`-source rules** run on the logical/physical line view.
4. **`Tree`-source rules** run on the typed AST projection of the CST (§12.2).
5. **`File`-source rules** run on the whole file.
6. **`Index`-source rules** run after the cross-file index is built (opt-in).
7. **Suppressions** are applied last (§6), then meta rules (§7.1).

### 12.5 Parser requirements

- Error recovery: a malformed region never aborts the parse; it is isolated and
  the rest of the file still lints. Only *valid* SQL that fails to parse is a
  bug; parsing SQL a database would reject is **not** a finding (the grammar is
  deliberately permissive, like the reference implementations).
- Trivia preservation: comments/whitespace round-trip such that
  `Unparse(Parse(src)) == src` byte-for-byte.
- No regex in hot paths; a single forward scan.
- `max-parse-depth` / `max-parse-nodes` bound pathological inputs.

### 12.6 Fix engine

Sort by start offset then stability → group into isolation groups → apply
greedily, non-overlapping → re-parse → if a syntax error is introduced, revert
that pass → repeat to `maxIterations = 100`. Deterministic order guaranteed.

### 12.7 Concurrency

A bounded worker pool sized to `runtime.NumCPU`, one file per worker, a single
sequential merge pass, and output sorted by path then range so parallel runs are
byte-identical to serial runs. **[decision]**

### 12.8 Dependencies

`spf13/cobra` + `pflag` (CLI), `pelletier/go-toml/v2` (config), `bmatcuk/doublestar/v4`
(globs), a gitignore matcher (e.g. `go-git`), `mattn/go-runewidth` (display
width), and `flosch/pongo2` for the jinja templater (see
[`TEMPLATING.md`](./TEMPLATING.md) §9). **[open]** — final selection of the
gitignore matcher.

---

## 13. Testing strategy

### 13.1 Layout

```
testdata/rules/<CODE>/<case>/{input.sql,expected.sql}   # rule + fix fixtures
testdata/rules/<CODE>/<case>.toml                       # per-case rule config
testdata/format/<case>/{input.sql,expected.sql}
testdata/dialects/<dialect>/*.sql + *.tree.json          # parse fixtures
testdata/corpus/                                         # large real-world sweep
testdata/config/                                         # discovery/precedence
testdata/cli/                                            # end-to-end CLI
```

### 13.2 Harnesses

- `internal/linttest`: `Run`, `AssertDiagnostics`, `AssertFix` — table-driven
  `TestRule_<CODE>` with positive, negative, and fix cases.
- `internal/formatest`: `Format`, `AssertFormat`, `AssertIdempotent`.

### 13.3 Test types

Rule fixtures · formatter goldens · parse fixtures per dialect · property tests
(`Unparse(Parse(x)) == x` and `Format(Format(x)) == Format(x)`) · fuzzers
(`FuzzParse`, `FuzzFormat`, `FuzzConfig`) · config discovery · CLI/e2e ·
corpus sweep. Goldens update with `-update`. **[decision]**

### 13.4 TDD order

Grammar/lexer → parser + round-trip property tests → tree/rules scaffolding →
rules (family by family, in catalog order) → fix engine → formatter → output → CLI
→ LSP. Each rule lands with its fixtures before it is enabled by default.

---

## 14. Roadmap

| Phase | Deliverable | Exit criteria |
|---|---|---|
| **M0 — Foundation** | module, lexer, CST parser, ANSI grammar, round-trip property tests | `Unparse(Parse(x)) == x` on the corpus |
| **M1 — Core lint** | `check` + the core rule families (AL/AM/CP/CV/LT/RF/ST) + fix engine + `concise` output | ≥ 60 rules, safe fixes, exit codes |
| **M2 — Formatter** | document IR, width-aware printer, idempotency | `Format(Format(x)) == Format(x)`, never introduces a `check` finding |
| **M3 — Breadth** | all 27 dialects, templaters (raw/placeholder/jinja/python), all output formats, config discovery, caching, watch | dialect parse fixtures green; `--dialect`/`--templater` end-to-end |
| **M4 — Integration** | LSP server, pre-commit hooks, shell completions, VS Code client, `rusq init`/`rule`/`config`/`dialects` | editor and CI parity with the CLI |
| **M5 — Parity & beyond** | full 79-rule compatibility baseline + native/dialect families (≥ 120 rules), optional-Python templaters, cross-file index | catalog complete; `RULES.md` frozen |

---

## 15. Resolved decisions & open questions

**Resolved:** binary/config stem `rusq` (§1.3); TOML-only config (§3.2);
selector precedence (§3.3); exit codes 0/1/2 (§2.4); 27 dialects (§8.1);
compatibility rule codes + native `RUS` meta family (§7.1); linter/formatter
separation invariant (§7.4); three-tier fix safety (§7.5); Line-length default
88 (§10.4); deterministic parallel output (§12.7); lossless CST as canonical
tree with a typed AST projection for structural rules (§12.2); Ruff-like minimal
default set — `correctness`/`suspicious`/`security` categories on, all others
opt-in (§7.1, §7.3).

**Open:** §9 whether the optional-Python templaters (dbt/SQLMesh) ship in v1 or
v1.1; §12.8 final gitignore-matcher and Jinja-engine dependency selection; §3.2
whether any INI compatibility reader is ever added; whether `soql` (Salesforce)
and other exotic dialects join the 27.

---

*This specification is a living document. When implementation refines a claim,
the claim moves here first and the code follows.*