# rusq — Product Requirements Document (PRD)

> A single, native-speed SQL linter and formatter in the spirit of
> [Ruff](https://docs.astral.sh/ruff/): **the breadth of coverage of the mature
> Python SQL linter, without the interpreter.**

Status: **draft v0.1** — the authoritative product requirement. The technical
design lives in [`SPEC.md`](./SPEC.md) (templating internals in
[`TEMPLATING.md`](./TEMPLATING.md), the rule catalog in [`RULES.md`](./RULES.md),
the dialect registry in [`DIALECTS.md`](./DIALECTS.md), editor/CI integration in
[`EDITOR.md`](./EDITOR.md), performance budgets in
[`PERFORMANCE.md`](./PERFORMANCE.md)); this document defines *who* it is for,
*what* it must do, and *how we know it worked*. Requirements are numbered
(`FR-*`, `NFR-*`) and prioritised with MoSCoW (`must` / `should` / `could` /
`won't`).

---

## 1. Summary

`rusq` is an open-source, single-binary SQL linter and formatter written in Go.
It parses SQL into a lossless tree, lints it against a large catalog of rules,
and formats it from the same tree — with byte-exact diagnostics, safe automatic
fixes, and coverage across many SQL dialects and templating layers.

Its reason to exist is a specific gap in the ecosystem: the de-facto standard
SQL linter is excellent in **breadth** but is a **Python** program, so it is slow
to install and slow to run on large projects; a newer **Rust** reimplementation
traded breadth for speed and is still filling in coverage. `rusq` takes the third
position: **match the reference's breadth in a fast, dependency-free Go binary**,
reusing the reference's rule codes so existing configurations migrate unchanged.

The author has already shipped a production Go linter/formatter for a different
domain using this exact playbook (single binary, one lossless parser shared by a
`check` and a `format` command, hierarchical TOML config, an LSP server,
pre-commit hooks). `rusq` applies that proven playbook to SQL.

---

## 2. Problem statement

SQL is the most widely written *and least well-linted* language in most data
stacks. Teams get ruff/ESLint/gofmt for their application code, then hand-write
10,000-line SQL models with **no automated review at all**. The consequences:

- **Silent correctness bugs.** `LEFT JOIN` written as `RIGHT JOIN`, `NOT IN`
  with `NULL`s, `COUNT(*)` vs `COUNT(column)`, implicit cross joins, `UNION`
  without `ALL` semantics, `GROUP BY` on a non-unique key. These change results
  and are not syntax errors, so no database or IDE catches them.
- **Reviewer fatigue.** Style debates (keyword case, comma position, alias
  rules) consume review time that should go to logic.
- **Slow feedback.** The one mature SQL linter covers a lot, but it is Python:
  installing it drags in an interpreter and dependency tree, and running it on a
  large project is slow enough that it is a *CI job*, not a *keystroke*. The
  Rust reimplementation is much faster but is younger, and its rule coverage and
  templater/dialect breadth are still catching up.

The market has therefore split **breadth vs. speed**. No mainstream tool delivers
both in a form that installs with a single `go install`/binary copy and runs on
every save.

---

## 3. Objective

Deliver a SQL linter and formatter that is:

1. **Broad** — coverage that matches the reference rule catalog, across many
   dialects and templating layers.
2. **Fast** — native, parallel, dependency-free; fast enough to be a save hook.
3. **Portable** — one self-contained binary; `go install` and done.
4. **Familiar** — reuses the reference's rule codes and the Ruff CLI/UX
   vocabulary, so adoption is a config change, not a rewrite.
5. **Complete** — lints *and* formats, and offers both through one parser and
   one config.

---

## 4. Goals and non-goals

### 4.1 Goals

- **G-1** Match the reference Python linter's rule catalog (79 rules) as the
  compatibility baseline, then exceed it in native and dialect families.
- **G-2** Support ≥ 27 SQL dialects with per-dialect grammar and version gating.
- **G-3** Support the templating layers real projects use: raw, placeholder,
  Jinja, Python (best-effort), with optional dbt/SQLMesh.
- **G-4** Provide an idempotent formatter that never introduces a lint finding.
- **G-5** Ship a first-class CLI (Ruff-style), an LSP server, pre-commit hooks,
  shell completions, and CI output formats.
- **G-6** Install and run with zero non-Go runtime dependencies.

### 4.2 Non-goals

- **NG-1** Not a database client, executor, or planner.
- **NG-2** Not a schema/type validator against a live catalog (v1).
- **NG-3** Not a Python-dependent tool in its core; optional templaters only.
- **NG-4** Not a data/schema migration tool.
- **NG-5** Not a query optimizer.

---

## 5. Target users and personas

### P1 — Analytics engineer (primary)
Writes dbt/SQLMesh models daily. Wants keyword-case and comma rules handled
automatically, correctness bugs caught before a model ships, and a formatter
that ends style debates. Values: speed on save, low config, editor integration.

### P2 — Data platform / analytics engineer
Maintains the warehouse and the conventions. Needs one tool that spans dialects
(Snowflake, BigQuery, Postgres), enforces a house style, and gates CI. Values:
config-as-code, per-directory overrides, machine-readable output, determinism.

### P3 — Backend / application engineer
Writes SQL embedded in Python/Go/Java. Wants to lint raw query strings and
migration files without adding a Python dependency to the build.

### P4 — Data engineer / DBA
Reviews DDL and migrations. Wants structure, convention, and dialect-security
rules (e.g. lock-avoidance, `NOT VALID` foreign keys) and an audit mode for
legacy code.

### P5 — Tooling author / platform integration
Embeds `rusq` in editors and pipelines. Needs LSP, stable exit codes, stable
machine output, and a library-quality internal API.

---

## 6. Use cases / user stories

- **UC-1** *As P1*, I run `rusq check .` on save and see correctness bugs inline.
- **UC-2** *As P1*, I run `rusq format .` and my models are consistently styled.
- **UC-3** *As P1*, I use Jinja `{{ ref(...) }}`, `{% if %}`, and macros; the
  linter still finds real issues and `JJ01` keeps template tags tidy.
- **UC-4** *As P2*, I add `--dialect snowflake` (or set it per directory) and get
  dialect-correct rules; my `--select`/`--ignore` lists from my old tool work
  unchanged.
- **UC-5** *As P2*, my CI runs `rusq check --output-format sarif` and
  `rusq format --check`, and fails on findings or drift.
- **UC-6** *As P3*, I run `rusq check --templater placeholder --dialect postgres`
  over query strings in a repo with no Python installed.
- **UC-7** *As P4*, I run `rusq check --select postgres` to catch DDL hazards and
  `rusq check --ignore-suppressions .` to audit legacy suppressions.
- **UC-8** *As P4*, I add `--add-noqa` to grandfather existing findings and fix
  them incrementally.
- **UC-9** *As P5*, I start `rusq server` and get diagnostics, quick-fixes,
  formatting, and range formatting over LSP.
- **UC-10** *As any user*, `rusq config`, `rusq rule <CODE>`, `rusq dialects`, and
  `-h` answer my questions without leaving the terminal.

---

## 7. Competitive landscape (positioning)

We compare on a two-axis map — **breadth** and **speed/deployability** — and do
not name the specific tools, only the categories:

| Category | Breadth | Speed / deploy | Templating | Dialects | Native binary |
|---|---|---|---|---|---|
| Mature Python reference linter | ★★★★★ | ★★ (interpreter, slow) | ★★★★★ | ★★★★★ | ✗ |
| Newer Rust reimplementation | ★★★ (catching up) | ★★★★★ | ★★★ (optional Python) | ★★★★ | ✓ |
| Legacy style-only formatters | ★ (style) | ★★★★ | ★ | ★★ | mixed |
| **rusq** | **★★★★★ (target)** | **★★★★★** | **★★★★ (native jinja; optional dbt)** | **★★★★★ (27)** | **✓** |

**Positioning statement.** *For teams that want the coverage of the established
SQL linter but refuse to run Python in their loop, `rusq` is a single Go binary
that lints and formats SQL across 27 dialects and the templating layers real
projects use — reusing the established rule codes so your existing configuration
keeps working.*

**Differentiators.**
1. **Breadth + native speed together** — the gap nobody has closed.
2. **Drop-in codes** — existing `--select`/`--ignore` and `noqa` directives work.
3. **One binary, one config, one parser** for both lint and format.
4. **Optional, not required, Python** — real Jinja in Go; dbt/SQLMesh only if you
   already run them.
5. **Proven playbook** — the author's prior Go tool demonstrates the exact
   architecture, packaging, LSP, and pre-commit story end to end.

---

## 8. Functional requirements

### 8.1 CLI

- **FR-CLI-1 (must)** `check` subcommand as default; `format`, `parse`, `rule`,
  `config`, `dialects`, `templaters`, `init`, `clean`, `server`, `version`,
  `help`, `generate-shell-completion`.
- **FR-CLI-2 (must)** Exit codes `0` clean / `1` findings / `2` abnormal.
- **FR-CLI-3 (must)** Selection flags `--select/-ignore/-extend-select/-extend-ignore`,
  `--per-file-ignores`, `--fixable/-unfixable`, `--warnings`, `--preview`.
- **FR-CLI-4 (must)** Fix flags `--fix`, `--unsafe-fixes`, `--diff`,
  `--fix-only`, `--show-fixes`, `--add-ignore`/`--add-noqa`.
- **FR-CLI-5 (must)** stdin (`-`) with `--stdin-filename`.
- **FR-CLI-6 (must)** `--dialect` and `--templater`.
- **FR-CLI-7 (must)** Ten output formats (concise/full/json/json-lines/github/
  gitlab/junit/sarif/grouped/azure), `-o/--output-file`, `--statistics`.
- **FR-CLI-8 (should)** `--watch`, `--show-files`, `--show-settings`, colour
  control, `--isolated`, `--config`, verbosity flags.

### 8.2 Configuration

- **FR-CFG-1 (must)** `.rusq.toml`/`rusq.toml` with hierarchical closest-wins
  discovery, `extend`, XDG fallback, and per-file-ignores.
- **FR-CFG-2 (must)** Full selector grammar with precedence
  `ALL < category < group < prefix < code`; deprecated aliases resolved.
- **FR-CFG-3 (must)** Per-dialect and per-rule option tables; per-directory
  dialect/templater.
- **FR-CFG-4 (should)** `rusq init` scaffolding; `rusq config [KEY]` introspection;
  `--show-settings` per-file effective settings.

### 8.3 Rule engine and catalog

- **FR-RULE-1 (must)** ≥ 79 rules = the reference compatibility baseline.
- **FR-RULE-2 (must)** ≥ 120 total catalogued rules by v1.0 (baseline + native
  `RUS` meta family + broadened dialect families).
- **FR-RULE-3 (must)** Every rule declares code, name, family, category,
  severity, cheapest lint source, fixability, default state, dialect
  applicability, and config keys.
- **FR-RULE-4 (must)** Safe/unsafe/display fix taxonomy; `--fix` applies safe by
  default; unsafe requires opt-in.
- **FR-RULE-5 (must)** Suppression directives (`ignore`, `disable`/`enable`,
  `file-ignore`, `fmt off/on`, `skip`) plus meta rules `RUS101`–`RUS104`.
- **FR-RULE-6 (must)** Cheapest-first pass schedule (token → lines → tree → file
  → index → suppressions).
- **FR-RULE-7 (must)** Cross-file/structural rules (unused CTE, unreferenced
  join, from-reference existence, set-column count) as part of the core families.

### 8.4 Dialects

- **FR-DIA-1 (must)** ≥ 27 dialects, ANSI base, inheritance for kindred
  dialects, lazy registration.
- **FR-DIA-2 (must)** Per-dialect default casing, quote characters, segment set,
  and rule skips.
- **FR-DIA-3 (must)** Version gating via per-dialect `target-version`.
- **FR-DIA-4 (should)** Parse fixtures per dialect with round-trip property
  tests.

### 8.5 Templating

Design: [`TEMPLATING.md`](./TEMPLATING.md).

- **FR-TMP-1 (must)** `raw` and `placeholder` (all major param styles).
- **FR-TMP-2 (must)** `jinja` natively in Go via `pongo2`: macros, library path,
  dbt builtins, context, `ignore-templating`, `JJ01`.
- **FR-TMP-3 (must)** Two jinja modes — **render** (execute and trace output back
  to source) and **substitution** (execute nothing; every `if`/`else` branch is
  visible) — with automatic fallback to substitution so templating never aborts
  a run.
- **FR-TMP-4 (must)** Source mapping: diagnostics locate in the original file;
  fixes touching a templated slice are display-only.
- **FR-TMP-5 (must)** Multi-variant rendering (limit default 5) with source-mapped
  diagnostics and provenance.
- **FR-TMP-6 (should)** `python` best-effort (f-strings, `.format()`, context).
- **FR-TMP-7 (could)** `dbt` and `sqlmesh` optional bridges via an external
  Python install; absent it, degrade to jinja with reduced coverage.

### 8.6 Formatter

- **FR-FMT-1 (must)** `format`, `--check`, `--diff`, `--range`.
- **FR-FMT-2 (must)** Idempotency: `Format(Format(x)) == Format(x)`.
- **FR-FMT-3 (must)** Never introduce a `check` finding (invariant test).
- **FR-FMT-4 (must)** Line-length, indent unit/width, comma/operator position,
  keyword position, target policy, blank-line normalization, trailing newline.
- **FR-FMT-5 (must)** Unparseable regions left byte-identical.

### 8.7 Integration

- **FR-INT-1 (must)** `rusq server` LSP: diagnostics, whole/range formatting,
  code-action quick-fixes.
- **FR-INT-2 (must)** pre-commit hooks (`rusq-check`, `rusq-format`) with
  `language: golang`.
- **FR-INT-3 (should)** VS Code client; documented Neovim/Helix setup.
- **FR-INT-4 (should)** Shell completions (bash/zsh/fish/PowerShell).
- **FR-INT-5 (should)** GitHub/Forgejo Action usable in CI.

---

## 9. Non-functional requirements

- **NFR-PERF-1 (must)** Parse + lint a cold 20k-file project in **< 5 s** on a
  modern 8-core machine; **< 1 s** for a warm cache hit run.
- **NFR-PERF-2 (must)** Per-file lint latency **< 5 ms** median for typical
  (< 300-line) models, so editor-on-change feels instant.
- **NFR-PERF-3 (must)** Near-linear scaling to at least 8 cores, output
  byte-identical to a serial run.
- **NFR-COR-1 (must)** `Unparse(Parse(x)) == x` for all valid inputs (property +
  fuzz tested).
- **NFR-COR-2 (must)** Grammar is **permissive**: rejecting SQL a database would
  accept is a bug; accepting SQL a database would reject is *not*.
- **NFR-COR-3 (must)** Deterministic output ordering (path, then range).
- **NFR-PORT-1 (must)** Builds for linux/macOS/Windows on amd64/arm64; no
  non-Go runtime dependency for the core.
- **NFR-PORT-2 (must)** Single self-contained binary; `go install` works.
- **NFR-REL-1 (must)** Stable machine-readable output and stable exit codes
  across patch releases.
- **NFR-REL-2 (must)** Fix engine is bounded (`maxIterations`) and reverts any
  pass that introduces a syntax error.
- **NFR-UX-1 (must)** Self-documenting CLI; helpful diagnostics with help text.
- **NFR-SEC-1 (must)** No network access, no telemetry, no database connections.
- **NFR-QUAL-1 (must)** CI: `gofmt` check, `go vet`, race detector, and the full
  fixture/corpus/property/fuzz suite green.

---

## 10. Success metrics

| Metric | Target (v1.0) | Measurement |
|---|---|---|
| Rule catalog size | ≥ 120 (≥ 79 compat baseline) | `RULES.md` + `rusq rule` listing |
| Dialect coverage | 27 dialects with parse fixtures | `rusq dialects` + fixture suite |
| Cold-run time (20k files, 8 cores) | < 5 s | `PERFORMANCE.md` benchmark |
| Per-file median lint | < 5 ms | benchmark harness |
| Formatter idempotency | 100% on corpus | property test |
| Parser round-trip | 100% on corpus | property test |
| Compatibility | existing configs run unchanged | migration test suite |
| Adoption (leading indicator) | ≥ 3 external repos gate CI on it | public CI configs |

---

## 11. Release plan

| Milestone | Scope | Definition of done |
|---|---|---|
| **M0 — Foundation** | module, lexer, CST parser, ANSI grammar, round-trip tests | corpus round-trip green |
| **M1 — Core lint (alpha)** | `check`, core families (AL/AM/CP/CV/LT/RF/ST), fix engine, concise output | ≥ 60 rules; safe fixes; exit codes |
| **M2 — Formatter (beta)** | document IR + printer, `format`/`--check`/`--diff` | idempotent; never adds a finding |
| **M3 — Breadth (beta)** | 27 dialects, templaters, 10 output formats, config discovery, caching, watch | dialect + templater e2e green |
| **M4 — Integration (rc)** | LSP, pre-commit, completions, VS Code client, introspection commands | editor/CI parity |
| **M5 — v1.0 (GA)** | 79-rule compat baseline + native/dialect families (≥ 120), optional-Python templaters, cross-file index, docs frozen | catalog complete; SPEC/RULES/PRD stable |

Post-1.0 candidates: live-catalog semantic checks (NG-2 revisited), more exotic
dialects (e.g. SOQL), auto-migration assistance, and a hosted playground.

---

## 12. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| 27-dialect breadth is a large surface | schedule | ANSI base + inheritance/override; fixtures per dialect; lazy loading; ship dialects progressively within M3 |
| Jinja fidelity gaps vs. a Python engine | missed/incorrect findings | multi-variant rendering; `ignore-templating`; optional full-Python bridge for dbt/SQLMesh; document coverage honestly |
| Formatter churns massive diffs | adoption friction | `format --check` first; safe defaults; idempotency invariant; `--range` |
| Unsafe fixes changing semantics | trust | three-tier fix taxonomy; unsafe off by default; revert-on-parse-error; fixtures per fix |
| Rule-code compatibility drift | breaks migrations | treat the compat catalog as a frozen baseline; aliases for renames; deprecation notices |
| Scope creep from semantic checks | delays v1 | keep catalog/type checks explicitly out of v1 (NG-2) |
| Performance regressions | the core value prop | benchmark + corpus sweep in CI; cheapest-first pass schedule |

---

## 13. Open questions

- **OQ-1** Do the optional-Python templaters (dbt/SQLMesh) ship in v1.0 or 1.1?
- **OQ-2** Should a limited INI compatibility reader be added for migrating
  configs, or is TOML-only acceptable?
- **OQ-3** Which gitignore matcher is used? (Resolved: the Jinja engine is
  `pongo2` — see [`TEMPLATING.md`](./TEMPLATING.md) §9.)
- **OQ-4** Do exotic dialects (SOQL, others) join the 27, and on what cadence?
- **OQ-5** Is a hosted/playground surface in scope post-1.0?

---

## Appendix A — Decisions log

- Binary/config stem `rusq`; module `github.com/ppreeper/rusq`; config
  `.rusq.toml`; cache `.rusq_cache/`; suppression `rusq:` (+ `noqa:` alias).
- TOML-only config; hierarchical closest-wins discovery.
- Compatibility rule codes reused as-is; native `RUS` meta family added.
- Linter/formatter separation invariant; format never adds a finding.
- Three-tier fix safety; safe-by-default fixes.
- Line-length default 88; exit codes 0/1/2.
- Deterministic parallel output; permissive grammar; optional Python only.
- One parser emits a **lossless CST** (canonical, round-trips source) plus a
  **typed AST projection** for structural rules; the formatter and fixes read
  the CST. See [`SPEC.md`](./SPEC.md) §12.2.

*See [`SPEC.md`](./SPEC.md) for the authoritative technical design.*