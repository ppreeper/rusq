# rusq — Rule catalog

> The complete, versioned catalog of every rule `rusq` ships: codes, names,
> severity, fixability, category, default state, and dialect applicability.

Status: draft v0.1 — the catalog is versioned with the binary and this document
is the authoritative listing. `[decision]` marks a recorded choice; `[open]`
marks something still being settled.

Companion documents:

- [`README.md`](../README.md) — what `rusq` is and how to run it.
- [`PRD.md`](./PRD.md) — product requirements, personas, and success metrics.
- [`SPEC.md`](./SPEC.md) — the technical specification (CLI, config, rule
  system, architecture).
- [`TEMPLATING.md`](./TEMPLATING.md) — how templated SQL is rendered and traced.
- [`DIALECTS.md`](./DIALECTS.md) — the dialect registry, inheritance graph, and
  per-dialect notes, including which rules each dialect skips.
- [`EDITOR.md`](./EDITOR.md) — editor (LSP), CI, and pre-commit integration.
- [`PERFORMANCE.md`](./PERFORMANCE.md) — budgets, benchmark harness, and corpus
  timings.

---

## 1. How to read a rule

Every rule is identified by a **code** (`PREFIX` + digits), a dotted **name**
(`layout.long_lines`), and a **family** (the prefix's word). Rules are the unit
of selection, suppression, and reporting.

### 1.1 Codes, families, and the compatibility baseline

The core families (`AL`, `AM`, `CP`, `CV`, `JJ`, `LT`, `RF`, `ST`) reuse the
codes established by the mature Python SQL linter, so a repository migrating to
`rusq` keeps its `--select`/`--ignore` lists and inline `noqa` directives working
unchanged. Legacy numeric aliases (e.g. `L016` for `LT05`) are accepted anywhere
a code is (see §5). Dialect families (`PG`, `OR`, `TQ`, and the extensions
`MY`, `BQ`, `SF`, `RS`, `CH`, `TD`, `TR`, `DG`, `SP`) follow the same shape.
The native meta family uses a three-digit code (`RUS101`) so it can never collide
with the two-digit core namespace. **[decision]**

### 1.2 Columns

| Column | Values | Meaning |
|---|---|---|
| **Code** | `LT05` | Stable identifier; canonical selector. |
| **Name** | `layout.long_lines` | Human-readable dotted name; also a selector. |
| **Sev** | `error` · `warning` · `hint` | Report severity (does not change exit code). |
| **Fix** | `safe` · `unsafe` · `display` · `—` | Auto-fix applicability (see §1.5). |
| **Cat** | category | Classification that decides the default set (see §1.4). |
| **Def** | `✓` · `—` | Is the rule on in the default set? |
| **Dialects** | `all` · list | Dialects the rule applies to; elsewhere it is skipped. |

### 1.3 Severity

Severity is a rendering attribute, not an exit-code switch: any finding exits `1`
(`0` clean, `2` abnormal — SPEC §2.4). The mapping is a policy, applied
uniformly:

| Category | Severity |
|---|---|
| `correctness` | `error` |
| `suspicious`, `security` | `warning` |
| `style`, `restriction` | `warning` |
| `pedantic`, `formatting` | `hint` |

`--warnings` (or its config equivalent) can downgrade selected rules to
`warning` so they are reported but never fail a CI run. **[decision]**

### 1.4 Categories and the default set

A rule's category decides whether it is on by default. The default set is
deliberately small and Ruff-like: `rusq check` catches likely bugs out of the
box and leaves opinionated style to opt-in. **[decision]**

| Category | Default | Intent |
|---|---|---|
| `correctness` | **on** | The SQL is wrong or produces a wrong result. |
| `suspicious` | **on** | Likely a mistake, but not provably wrong. |
| `security` | **on** | Operationally dangerous (locks, unvalidated constraints). |
| `style` | off | Formatting/consistency preferences (capitalisation, aliasing). |
| `formatting` | off | Mirrors the formatter; opt-in for lint-only workflows. |
| `pedantic` | off | Minor nitpicks with low signal. |
| `restriction` | off | Forbids otherwise-valid constructs. |

Anyone who wants the full style surface turns it on explicitly, e.g.
`--select CP,LT` or `--select style`. Selecting an off-by-default rule never
*changes* how it behaves — it only makes it reportable. Rules that mirror the
formatter (`formatting`) are off so `check` and `format` never double-report
(SPEC §7.4).

### 1.5 Fix safety

| Value | Applied by | Meaning |
|---|---|---|
| `safe` | `--fix` | Semantics-preserving; always applied. |
| `unsafe` | `--fix --unsafe-fixes` | May change behaviour; opt-in. |
| `display` | never automatically | Shown in `--diff`/`full`; hand-applied. |
| `—` | — | No fix; a report-only rule. |

The engine sorts edits by start offset then stability, applies them greedily and
non-overlapping within isolation groups, iterates to `maxIterations = 100`, and
reverts any pass that introduces a syntax error (SPEC §7.5). Fixes that touch a
templated slice are downgraded to `display` (TEMPLATING.md §5).

### 1.6 Dialect applicability and version gating

A rule that is meaningless on a dialect is **skipped, not errored** (SPEC §8.1).
`all` means every dialect; otherwise the rule runs only on the listed dialects.
Some rules are *enabled* by default only on certain dialects; those carry a note
and a `force_enable`/`force-disable` config key. Rules that depend on a
construct introduced in a specific dialect version run only when `target-version`
warrants it (SPEC §8.2). The dialect registry, inheritance graph, quoting model,
and per-dialect skip lists are in [`DIALECTS.md`](./DIALECTS.md), which is the
reference for every `dialects` cell in the catalog below.

---

## 2. Aliasing — `AL`

Rules about table and column aliasing: whether aliases exist, whether they are
explicit, unique, used, and how long they may be.

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `AL01` | `aliasing.table` | warning | safe | style | — | Implicit/explicit `AS` on a table alias (explicit preferred). |
| `AL02` | `aliasing.column` | warning | safe | style | — | Implicit/explicit `AS` on a column alias (explicit preferred). |
| `AL03` | `aliasing.expression` | warning | display | style | — | A calculated `SELECT` target has no alias; one is required. |
| `AL04` | `aliasing.unique.table` | error | — | correctness | ✓ | Table aliases must be unique within a clause. |
| `AL05` | `aliasing.unused` | warning | safe | suspicious | — | A table/CTE alias is declared but never referenced. |
| `AL06` | `aliasing.length` | hint | — | pedantic | — | Table-alias length within bounds. |
| `AL07` | `aliasing.forbid` | warning | unsafe | restriction | — | Table aliases are forbidden in `FROM`/`JOIN`. |
| `AL08` | `aliasing.unique.column` | error | — | correctness | ✓ | Column aliases must be unique within a clause. |
| `AL09` | `aliasing.self_alias.column` | warning | safe | style | — | A column is aliased to itself (redundant rename). |
| `AL10` | `aliasing.required` | error | — | correctness | ✓ | A derived table (subquery in `FROM`) must have an alias. |

**Options**

- `AL01`/`AL02` — `aliasing = explicit | implicit` (`explicit` default).
- `AL03` — `allow_scalar = true | false` (default `true`; a lone scalar is allowed).
- `AL05` — `alias_case_check = dialect | case_insensitive | case_sensitive | quoted_cs_naked_ci | quoted_ci_naked_cs` (default `dialect`).
- `AL06` — `min_alias_length`, `max_alias_length` (both default `0` = off).
- `AL07` — `force_enable = true | false` (default `false`; the rule is off by default).

`AL04`/`AL08` compare aliases case-insensitively when the dialect folds unquoted
identifiers; quoted aliases are always compared as written.

---

## 3. Ambiguous — `AM`

Rules about constructs whose meaning is unclear or whose shape hides a mistake:
set operations, `DISTINCT`, joins, ordering, and column counts.

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `AM01` | `ambiguous.distinct` | warning | — | suspicious | ✓ | `DISTINCT` in a `SELECT` that also has `GROUP BY`. |
| `AM02` | `ambiguous.union` | warning | safe | suspicious | ✓ | Prefer explicit `UNION DISTINCT`/`UNION ALL` over bare `UNION`. |
| `AM03` | `ambiguous.order_by` | warning | safe | suspicious | ✓ | Mixed/absent ordering directions in a multi-column `ORDER BY`. |
| `AM04` | `ambiguous.column_count` | warning | — | suspicious | ✓ | `SELECT *` yields a result whose column count/order can change. |
| `AM05` | `ambiguous.join` | warning | safe | style | — | Join clauses should be fully qualified (`INNER JOIN`). |
| `AM06` | `ambiguous.column_references` | warning | — | style | — | Consistent implicit/explicit references in `GROUP BY`/`ORDER BY`. |
| `AM07` | `ambiguous.set_columns` | error | — | correctness | ✓ | Queries in a set operation return different column counts. |
| `AM08` | `ambiguous.join_condition` | warning | safe | suspicious | ✓ | A join has no `ON`/`USING` — an implicit cross join. |
| `AM09` | `ambiguous.order_by_limit` | warning | — | suspicious | ✓ | `LIMIT`/`OFFSET` without an `ORDER BY` is non-deterministic. |

**Options**

- `AM05` — `fully_qualify_join_types = inner | outer | both` (default `inner`).
- `AM06` — `group_by_and_order_by_style = consistent | explicit | implicit` (default `consistent`).

**Dialect notes** — `AM02` is skipped where `UNION DISTINCT` is not distinct from
`UNION` (`duckdb`, `exasol`, `postgres`, `sparksql`).

---

## 4. Capitalisation — `CP`

Rules that enforce a consistent letter case for keywords, identifiers,
functions, literals, and datatypes.

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `CP01` | `capitalisation.keywords` | warning | safe | style | — | Consistent capitalisation of keywords. |
| `CP02` | `capitalisation.identifiers` | warning | safe | style | — | Consistent capitalisation of unquoted identifiers. |
| `CP03` | `capitalisation.functions` | warning | safe | style | — | Consistent capitalisation of function names. |
| `CP04` | `capitalisation.literals` | warning | safe | style | — | Consistent capitalisation of boolean/`NULL` literals. |
| `CP05` | `capitalisation.types` | warning | safe | style | — | Consistent capitalisation of datatypes. |

**Options**

- `CP01` — `capitalisation_policy = consistent | upper | lower | capitalise`.
- `CP02`/`CP03`/`CP05` — `extended_capitalisation_policy = consistent | upper | lower | pascal | camel | snake`.
- All — `ignore_words`, `ignore_words_regex`.
- `CP02` — `unquoted_identifiers_policy = all | aliases | column_aliases | table_aliases`.

`consistent` (the default) infers the prevailing style from the file's first
occurrence and flags deviations without imposing a case; the explicit policies
impose one. Dialect default casing (SPEC §8.1) seeds the inference.

---

## 5. Convention — `CV`

Rules about established idioms and cross-dialect conventions.

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `CV01` | `convention.not_equal` | warning | safe | style | — | Consistent `!=` or `<>` for "not equal". |
| `CV02` | `convention.coalesce` | warning | safe | style | — | Prefer `COALESCE` over `IFNULL`/`NVL`. |
| `CV03` | `convention.select_trailing_comma` | warning | safe | style | — | Trailing-comma policy in the `SELECT` clause. |
| `CV04` | `convention.count_rows` | warning | safe | style | — | Consistent `COUNT(*)`/`COUNT(1)`/`COUNT(0)`. |
| `CV05` | `convention.is_null` | error | safe | correctness | ✓ | Compare `NULL` with `IS`/`IS NOT`, never `=`/`!=`. |
| `CV06` | `convention.terminator` | warning | safe | style | — | Statements must end with a semicolon. |
| `CV07` | `convention.statement_brackets` | warning | safe | style | — | A top-level statement is not wrapped in brackets. |
| `CV08` | `convention.left_join` | warning | unsafe | style | — | Prefer `LEFT JOIN` over `RIGHT JOIN`. |
| `CV09` | `convention.blocked_words` | warning | — | restriction | — | Configurable list of forbidden words. |
| `CV10` | `convention.quoted_literals` | warning | safe | style | — | Consistent preferred quote character for literals. |
| `CV11` | `convention.casting_style` | warning | safe | style | — | Consistent type-casting style (`CAST`/`::`/`CONVERT`). |
| `CV12` | `convention.join_condition` | warning | unsafe | style | — | A join condition belongs in `ON`, not `WHERE`. |
| `CV13` | `convention.last_select_star` | warning | — | style | — | The final `SELECT` of a CTE model should be `SELECT * FROM …`. |

**Options**

- `CV01` — `preferred_not_equal_style = consistent | c_style | ansi`.
- `CV03` — `select_clause_trailing_comma = forbid | require`.
- `CV04` — `prefer_count_1`, `prefer_count_0` (`prefer_count_1` wins if both set).
- `CV06` — `multiline_newline = true | false`, `require_final_semicolon = true | false`.
- `CV09` — `blocked_words`, `blocked_regex`, `match_source`.
- `CV10` — `preferred_quoted_literal_style = consistent | single_quotes | double_quotes`, `force_enable`.
- `CV11` — `preferred_type_casting_style = consistent | cast | convert | shorthand`.
- `CV13` — `force_enable = true | false` (off unless enabled).

**Dialect notes** — `CV01` is skipped on `teradata`; `CV11` is skipped on
`athena`, `teradata`, and `trino`, where the casting forms are not interchangeable.

---

## 6. Jinja — `JJ`

Rules about the templating layer itself. `JJ` only activates when the `jinja`
templater (or a subclass such as `dbt`) is in use; under any other templater the
family is a no-op, not an error (SPEC §9, TEMPLATING.md §11). `JJ` rules run on
the **raw source**, not on rendered variants.

| Code | Name | Sev | Fix | Cat | Def | Dialects | Description |
|---|---|---|---|---|---|---|---|
| `JJ01` | `jinja.padding` | warning | safe | style | — | jinja | Jinja tags have exactly one space inside `{{ }}`/`{% %}`. |

---

## 7. Layout — `LT`

Layout rules describe where whitespace, newlines, indentation, and commas go.
They mirror the formatter (SPEC §10) so a lint-only user can enforce the same
style; when `format` and these rules both run, the formatter wins and the rule
does not double-report (SPEC §7.4). The whole family is `formatting` and off by
default. **[decision]**

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `LT01` | `layout.spacing` | hint | safe | formatting | — | Inappropriate spacing and trailing whitespace. |
| `LT02` | `layout.indent` | hint | safe | formatting | — | Incorrect indentation (mixes spaces/tabs, wrong depth). |
| `LT03` | `layout.operators` | hint | safe | formatting | — | Operators placed before/after newlines per policy. |
| `LT04` | `layout.commas` | hint | safe | formatting | — | Leading/trailing comma enforcement. |
| `LT05` | `layout.long_lines` | hint | safe | formatting | — | A line exceeds the configured length. |
| `LT06` | `layout.functions` | hint | safe | formatting | — | A function name is separated from its `(` by a space. |
| `LT07` | `layout.cte_bracket` | hint | safe | formatting | — | A `WITH` clause's closing bracket should be on a new line. |
| `LT08` | `layout.cte_newline` | hint | safe | formatting | — | Blank line expected after a CTE's closing bracket. |
| `LT09` | `layout.select_targets` | hint | safe | formatting | — | Select targets on new lines (unless a lone target). |
| `LT10` | `layout.select_modifiers` | hint | safe | formatting | — | `SELECT` modifiers (`DISTINCT`) on the `SELECT` line. |
| `LT11` | `layout.set_operators` | hint | safe | formatting | — | Set operators (`UNION`) surrounded by newlines. |
| `LT12` | `layout.end_of_file` | hint | safe | formatting | — | A file ends with exactly one trailing newline. |
| `LT13` | `layout.start_of_file` | hint | safe | formatting | — | A file does not begin with newlines or whitespace. |
| `LT14` | `layout.keyword_newline` | hint | safe | formatting | — | Keyword clauses follow the before/after-newline policy. |
| `LT15` | `layout.newlines` | hint | safe | formatting | — | Too many consecutive blank lines. |

**Options** — most layout options live in the shared `[layout]` config section
(SPEC §10.4) rather than per rule:

- `LT05` — inherits `line-length` (default 88); plus `ignore_comment_lines`,
  `ignore_comment_clauses`.
- `LT09` — `wildcard_policy = single | multiple`, `single_target_policy = same_line | new_line`.
- `LT03`/`LT04`/`LT14` — `line-position` / `keyword-line-position`
  (`leading` | `trailing` | `alone` | `leading:attached` | `trailing:attached` |
  `leading:align-following`).
- `LT15` — `maximum_empty_lines_between_statements`,
  `maximum_empty_lines_inside_statements`, `maximum_empty_lines_between_batches`,
  `minimum_empty_lines_between_statements`.

---

## 8. References — `RF`

Rules about whether column and table references resolve, are qualified, and use
safe identifiers.

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `RF01` | `references.from` | error | — | correctness | ✓ | A reference names an object not present in `FROM`. |
| `RF02` | `references.qualification` | warning | — | suspicious | ✓ | Qualify references when more than one table is in scope. |
| `RF03` | `references.consistent` | warning | safe | style | — | Column references are qualified consistently. |
| `RF04` | `references.keywords` | warning | — | style | — | Keywords are not used as identifiers. |
| `RF05` | `references.special_chars` | warning | — | style | — | Identifiers contain no special characters. |
| `RF06` | `references.quoting` | warning | safe | style | — | An identifier is quoted unnecessarily. |
| `RF07` | `references.window_alias` | error | — | correctness | ✓ | A `SELECT` alias is referenced inside its own `OVER` clause. |

**Options**

- `RF01` — `force_enable = true | false`.
- `RF02` — `subqueries_ignore_external_references = true | false`.
- `RF03` — `single_table_references = consistent | qualified | unqualified`, `force_enable`.
- `RF04` — `unquoted_identifiers_policy`, `quoted_identifiers_policy`, `ignore_words`, `ignore_words_regex`.
- `RF05` — `quoted_identifiers_policy`, `unquoted_identifiers_policy`, `allow_space_in_identifier`, `additional_allowed_characters`, `ignore_words`, `ignore_words_regex`.
- `RF06` — `prefer_quoted_identifiers`, `prefer_quoted_keywords`, `prefer_quoted_keyword_style`, `ignore_words`, `ignore_words_regex`, `case_sensitive`.
- `RF07` — `force_enable = true | false`.

**Dialect notes** — `RF01` is disabled by default on dialects with dynamic
namespace resolution (`athena`, `bigquery`, `databricks`, `duckdb`, `hive`,
`redshift`, `sparksql`, and `soql` where available) because it would produce
false positives; `RF03` is disabled by default on `athena`, `bigquery`, `hive`,
and `redshift` for the same reason. Use `force_enable` to turn them on.

---

## 9. Structure — `ST`

Rules about the shape of a statement: redundant or misordered constructs, and
CTE/join usage.

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `ST01` | `structure.else_null` | warning | safe | style | — | Redundant `ELSE NULL` in a `CASE`. |
| `ST02` | `structure.simple_case` | warning | safe | style | — | A `CASE` that returns booleans could be a predicate. |
| `ST03` | `structure.unused_cte` | warning | — | suspicious | ✓ | A CTE is defined but never referenced. |
| `ST04` | `structure.nested_case` | warning | safe | style | — | A `CASE` nested in `ELSE` could be flattened. |
| `ST05` | `structure.subquery` | warning | unsafe | style | — | Subqueries in `JOIN`/`FROM` should be CTEs. |
| `ST06` | `structure.column_order` | warning | unsafe | style | — | Wildcards and simple targets precede calculations/aggregates. |
| `ST07` | `structure.using` | warning | unsafe | style | — | Prefer explicit join keys over `USING`. |
| `ST08` | `structure.distinct` | warning | safe | style | — | `DISTINCT` is used with parentheses. |
| `ST09` | `structure.join_condition_order` | warning | unsafe | style | — | Join conditions list the earlier/later table first, consistently. |
| `ST10` | `structure.constant_expression` | warning | — | suspicious | ✓ | A redundant expression always evaluates to `TRUE`/`FALSE`. |
| `ST11` | `structure.unused_join` | warning | — | suspicious | ✓ | A joined table is never referenced. |
| `ST12` | `structure.consecutive_semicolons` | warning | safe | suspicious | ✓ | A run of two or more semicolons (likely a stray statement). |

**Options**

- `ST05` — `forbid_subquery_in = join | from | both` (default `join`).
- `ST09` — `preferred_first_table_in_join_clause = earlier | later` (default `earlier`).

---

## 10. Dialect families

Dialect rules encode behaviour that is only meaningful — or only safe — on one
engine. They follow the same code shape as the core families and are selected,
suppressed, and reported identically.

### 10.1 `PG` — PostgreSQL

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `PG01` | `postgres.excessive_locks` | warning | — | security | ✓ | Avoid DDL that takes a lock blocking reads/writes for the whole operation. |
| `PG02` | `postgres.not_valid_foreign_key` | warning | — | security | ✓ | Add a foreign key `NOT VALID`, then `VALIDATE` separately. |

Both carry `force_enable` (default `true` on `postgres`; skipped elsewhere).

### 10.2 `OR` — Oracle

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `OR01` | `oracle.empty_batch` | warning | safe | style | — | Remove an empty batch (a lone `/`). |

### 10.3 `TQ` — T-SQL

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `TQ01` | `tsql.sp_prefix` | warning | — | style | — | User procedures must not use the reserved `sp_` prefix. |
| `TQ02` | `tsql.procedure_begin_end` | warning | safe | style | — | Multi-statement procedure bodies are wrapped in `BEGIN`/`END`. |
| `TQ03` | `tsql.empty_batch` | warning | safe | style | — | Remove an empty batch (containing only `GO`). |
| `TQ04` | `tsql.prefer_as_alias` | warning | safe | style | — | Prefer ANSI `AS` aliasing over `alias = expression`. |

`TQ04` carries `force_enable` (default `true` on `tsql`).

### 10.4 Extension families (reserved)

The following prefixes are reserved for `rusq` dialect rules beyond the
compatibility baseline. Codes are allocated as rules are specified; until then
the families exist so selectors (`--select MY`, `--select SF`) resolve to an
empty set rather than an error. **[decision]**

| Prefix | Family | Dialects | Planned focus |
|---|---|---|---|
| `MY` | `mysql` | mysql, mariadb | engine mismatch, `utf8` vs `utf8mb4`, unsafe DDL |
| `BQ` | `bigquery` | bigquery | partition/cluster requirements, `SELECT *` cost |
| `SF` | `snowflake` | snowflake | `QUALIFY` usage, warehouse/clustering hints |
| `RS` | `redshift` | redshift | distribution/sort-key discipline |
| `CH` | `clickhouse` | clickhouse | `FINAL`/engine pitfalls, `LowCardinality` |
| `TD` | `teradata` | teradata | `QUALIFY`, `PRIMARY INDEX` conventions |
| `TR` | `trino` | trino | predicate pushdown-unfriendly constructs |
| `DG` | `databricks` | databricks | Delta-specific DDL, `MERGE` shape |
| `SP` | `sparksql` | sparksql | partition hints, `CACHE`/`UNCACHE` |

---

## 11. Templater families

### 11.1 `JJ` — Jinja

See §6. Active only under the `jinja` (or `dbt`) templater.

### 11.2 `PL` — Placeholder (reserved)

`PL` is reserved for rules about placeholder-style parameters (SPEC §9). Like the
extension families, it resolves to an empty set until populated; it exists so
`--select PL` and templater-scoped suppression work.

---

## 12. Meta family — `RUS`

The native meta family polices the suppression mechanism itself: a directive that
names nothing, is unused, or points at a deprecated code. Three-digit codes keep
the two namespaces disjoint (SPEC §6, §7.1).

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `RUS101` | `rusq.invalid` | warning | — | correctness | ✓ | A suppression directive names an unknown rule code. |
| `RUS102` | `rusq.unused` | hint | safe | style | — | A suppression directive silences nothing (dead directive). |
| `RUS103` | `rusq.deprecated` | warning | safe | style | — | A suppression references a deprecated code; rewrite it. |
| `RUS104` | `rusq.parse_error` | error | — | correctness | ✓ | A suppression cannot be applied because the region did not parse. |

---

## 13. Cross-file rules

With `[index].enabled = true` (SPEC §8.3) `rusq` builds a project index of
table/CTE definitions and references. Cross-file rules — those whose evidence
lives in more than one file — run in the `Index` lint pass and are off until the
index is on. **[decision]**

| Code | Name | Sev | Fix | Cat | Def | Description |
|---|---|---|---|---|---|---|
| `ST03` | `structure.unused_cte` | warning | — | suspicious | ✓ | Extends to CTEs defined in one file and never referenced in the project. |
| `ST13` | `structure.duplicate_definition` | warning | — | suspicious | ✓ | `table`/`view` is defined in more than one file. |
| `ST14` | `structure.unused_definition` | warning | — | style | — | A view defined here is never referenced anywhere in the project. |
| `RF08` | `references.unknown_object` | warning | — | style | — | A `ref()`/`source()` target the project never defines. |

`ST13` reports once per duplicate — on every defining file except the first in
path order — so a table defined in three files yields two findings, both naming
the canonical file. CTE definitions are excluded: their names are
statement-local, so repeating one across files is not a project error. **[decision]**

`ST14` applies to views and materialized views only: tables are usually created
by a migration and consumed by application code outside the SQL corpus, so
reporting an "unused" table would be noise. A view that is defined more than once
is left to `ST13`, and a view that no file references is reported once, at its
definition. **[decision]**

`RF08` scans the raw source for `ref('name')` and `source('schema', 'table')`
calls, which the templater would otherwise resolve away before any rule sees
them, and reports a call whose target no file in the project defines. It stays
silent when the project defines nothing at all, so a repository whose models live
outside the SQL corpus is not flooded. **[decision]**

The index is stored at `.rusq_cache/index.json` and keyed by a content hash per
file plus the dialect, so an unchanged file is never re-parsed and editing one
file re-indexes only that file. The language server builds the same index from
the workspace root and records each open buffer in it, so cross-file findings
appear in the editor as the user types (EDITOR §4.4). Semantic checks against a
live database catalog remain out of scope for v1 (SPEC §1.2). **[decision]**

---

## 14. Summary

### 14.1 Counts by family

| Family | Prefix | Rules | Default-on |
|---|---|---|---|
| Aliasing | `AL` | 10 | 3 |
| Ambiguous | `AM` | 9 | 7 |
| Capitalisation | `CP` | 5 | 0 |
| Convention | `CV` | 13 | 1 |
| Jinja | `JJ` | 1 | 0 |
| Layout | `LT` | 15 | 0 |
| References | `RF` | 7 | 3 |
| Structure | `ST` | 12 | 4 |
| PostgreSQL | `PG` | 2 | 2 |
| Oracle | `OR` | 1 | 0 |
| T-SQL | `TQ` | 4 | 0 |
| **Baseline subtotal** | | **79** | **20** |
| Meta | `RUS` | 4 | 2 |
| Cross-file | `ST13`, `ST14`, `RF08` | 3 | 1 |
| **Catalogued (v0.1)** | | **86** | **23** |

`ST13`, `ST14` and `RF08` are not part of the compatibility baseline, so they are
counted separately. **[decision]**

### 14.2 Counts by category

| Category | Rules | Default |
|---|---|---|
| correctness | 9 | on |
| suspicious | 13 | on |
| security | 2 | on |
| style | 44 | off |
| formatting | 15 | off |
| pedantic | 1 | off |
| restriction | 2 | off |

### 14.3 Coverage target

v1.0 targets **≥ 120 catalogued rules**: the 79-rule compatibility baseline, the
4 native meta rules, the cross-file rules (§13), and roughly 40
extension/dialect rules across the reserved families (§10.4, SPEC §7.6). **[decision]**

---

## 15. Migration & legacy aliases

Every code a user may already have in a config or `noqa` comment is accepted.
The canonical codes are the ones in this document; the numeric aliases below are
accepted anywhere a code is and are rewritten by `--add-ignore`/`rusq rule`.

| Legacy | Canonical | Legacy | Canonical |
|---|---|---|---|
| `L010` | `CP01` | `L044` | `AM04` |
| `L011` | `AL01` | `L045` | `ST03` |
| `L012` | `AL02` | `L046` | `JJ01` |
| `L013` | `AL03` | `L047` | `CV04` |
| `L014` | `CP02` | `L048` | `LT01` |
| `L015` | `ST08` | `L049` | `CV05` |
| `L016` | `LT05` | `L050` | `LT13` |
| `L017` | `LT06` | `L051` | `AM05` |
| `L018` | `LT07` | `L052` | `CV06` |
| `L019` | `LT04` | `L053` | `CV07` |
| `L020` | `AL04` | `L054` | `AM06` |
| `L021` | `AM01` | `L055` | `CV08` |
| `L022` | `LT08` | `L056` | `TQ01` |
| `L023` | `LT01` | `L057` | `RF05` |
| `L024` | `LT01` | `L058` | `ST04` |
| `L025` | `AL05` | `L059` | `RF06` |
| `L026` | `RF01` | `L060` | `CV02` |
| `L027` | `RF02` | `L061` | `CV01` |
| `L028` | `RF03` | `L062` | `CV09` |
| `L029` | `RF04` | `L063` | `CP05` |
| `L030` | `CP03` | `L064` | `CV10` |
| `L031` | `AL07` | `L065` | `LT11` |
| `L032` | `ST07` | `L066` | `AL06` |
| `L033` | `AM02` | `L067` | `CV11` |
| `L034` | `ST06` | `L068` | `AM07` |
| `L035` | `ST01` | `L071` | `LT01` |
| `L036` | `LT09` | `layout.end-of-file` | `LT12` |
| `L037` | `AM03` | `L001` | `LT01` |
| `L038` | `CV03` | `L002` | `LT02` |
| `L039` | `LT01` | `L003` | `LT02` |
| `L040` | `CP04` | `L004` | `LT02` |
| `L041` | `LT10` | `L005` | `LT01` |
| `L042` | `ST05` | `L006` | `LT01` |
| `L043` | `ST02` | `L007` | `LT03` |
| | | `L008` | `LT01` |
| | | `L009` | `LT12` |

The rules that predate the numeric scheme entirely (`AL08`, `AL09`, `AL10`,
`AM08`, `AM09`, `CV12`, `CV13`, `RF07`, `ST09`–`ST12`, `ST13`, `ST14`, `RF08`,
`TQ02`–`TQ04`, `OR01`, `PG01`, `PG02`) have no legacy alias.

---

## 16. Extending the catalog

Adding a rule means adding one declarative entry and one test fixture:

1. Choose the family/prefix and the next free two-digit code (three-digit for
   `RUS`).
2. Declare `RuleMeta` — code, name, family, category, severity, `LintSource`,
   fixability, default, stability, dialects, and config keys (SPEC §7.2).
3. Implement `Check` at the cheapest sufficient pass — `Token` first, then
   `Lines`, `Tree` (the typed AST projection), `File`, and finally `Index`
   (SPEC §12.2, §12.4).
4. Add fixtures under `testdata/rules/<CODE>/` (`input`/`expected`, positive and
   negative, plus a fix case when fixable) and a `TestRule_<CODE>` table test
   (SPEC §13).
5. Update this document's family table and the §14 counts.

A rule is **stable** once its fixtures pass on the full dialect corpus; until
then it is **preview** and excluded from the default set regardless of category.
**[decision]**

---

## 17. Open questions

- **OQ-1** Exact allocation of the extension/dialect families (§10.4) — which
  codes ship in v1.0 versus v1.x.
- **OQ-2** Whether the `PL` (placeholder) family gains rules that inspect the
  parameter style itself, or stays an empty reserved namespace.
- **OQ-3** *(resolved)* Cross-file rules default as follows: `ST03` and `ST13`
  are on whenever the index is enabled; `ST14` and `RF08` are `style` and
  therefore opt-in, because both depend on a repository whose SQL corpus is
  self-contained.
- **OQ-4** Whether `style` should be split into `style` and `convention` so a
  user can enable one without the other.