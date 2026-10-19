# rusq — Dialect support

> A single ANSI base grammar, extended by inheritance into 27 dialects, so that
> `rusq` parses and lints the SQL people actually write — not just the subset
> the standard describes.

Status: draft v0.1 — authoritative design, intentionally opinionated. `[decision]`
marks a recorded choice; `[open]` marks an unresolved one. This document is the
companion to [`SPEC.md`](./SPEC.md) §8 and to the dialect families in
[`RULES.md`](./RULES.md) §10.

Companion documents:

- [`SPEC.md`](./SPEC.md) — the full technical specification.
- [`PRD.md`](./PRD.md) — the product requirements.
- [`RULES.md`](./RULES.md) — the rule catalog, including per-dialect rules.
- [`TEMPLATING.md`](./TEMPLATING.md) — rendering and source mapping.
- [`EDITOR.md`](./EDITOR.md) — editor (LSP), CI, and pre-commit integration.
- [`PERFORMANCE.md`](./PERFORMANCE.md) — budgets and benchmark methodology.

---

## 1. Design stance

A **dialect** is a named grammar profile: a lexer configuration plus a set of
segment definitions layered over the ANSI base. A dialect selects exactly five
things (SPEC §8.1):

1. **Default keyword casing** — `UPPERCASE` almost everywhere; the seed for the
   `CP` capitalisation rules when their policy is `consistent` (RULES §4).
2. **Quote characters** — which delimiter introduces an identifier and which a
   string literal, and whether a given delimiter is ambiguous.
3. **Valid segment types** — the statements, clauses, and expressions the
   grammar recognises.
4. **Skipped rules** — a rule that is meaningless on a dialect is *skipped, not
   errored*. Skipping is a property of the rule × dialect pair, not a fatality.
5. **`target-version` semantics** — how a version string is interpreted and
   which version-gated constructs are in play (§6).

Three invariants hold for every dialect:

- **One lexer, one parser, one CST.** A dialect never introduces a second parse
  path; it changes the grammar the single lossless parser consumes
  (SPEC §12.2). The typed AST projection is derived from the same CST.
- **Permissive, not validating.** The grammar accepts SQL a particular engine
  might reject. A construct the engine rejects is *not* a `rusq` bug; only
  *valid* SQL that fails to parse is. This keeps 27 dialects tractable.
- **Lazy registration.** Dialect definitions are registered but not imported by
  the core (SPEC §8.1). Selecting `--dialect postgres` loads `postgres` and its
  ancestor chain only.

### 1.1 Anatomy of a dialect

Every dialect file is small and declarative. It:

- names itself and its parent (`copy_as`),
- replaces or adds a handful of segments,
- extends the keyword and reserved-word sets,
- adjusts the lexer matchers for quoting and comments where they differ,
- records which rules it skips or enables/disables by default.

Kindred dialects override only what differs, so a fix to a shared construct
lands once in the ancestor. **[decision]**

---

## 2. The registry

27 dialects ship in v1. `ansi` is the base and the default. `parent` is the
dialect a definition is `copy_as`-derived from; `—` means the ANSI base.

| Dialect | Formatted name | Parent | Identifier quotes | Literal quotes | Unquoted ident case |
|---|---|---|---|---|---|
| `ansi` | ANSI | — | `"…"` | `'…'` | upper |
| `athena` | Amazon Athena | `ansi` | `` `…` ``, `"…"` | `'…'` | lower |
| `bigquery` | Google BigQuery | `ansi` | `` `…` `` | `'…'`, `"…"` | upper |
| `clickhouse` | ClickHouse | `ansi` | `` `…` ``, `"…"` | `'…'` | upper |
| `databricks` | Databricks | `sparksql` | `` `…` `` | `'…'` | upper |
| `db2` | IBM DB2 | `ansi` | `"…"` | `'…'` | upper |
| `doris` | Apache Doris | `mysql` | `` `…` `` | `'…'` | upper |
| `duckdb` | DuckDB | `postgres` | `"…"` | `'…'` | lower |
| `exasol` | Exasol | `ansi` | `"…"` | `'…'` | upper |
| `flink` | Apache Flink | `ansi` | `` `…` `` | `'…'` | upper |
| `greenplum` | Greenplum | `postgres` | `"…"` | `'…'` | lower |
| `hive` | Apache Hive | `ansi` | `` `…` `` | `'…'`, `"…"` | lower |
| `impala` | Apache Impala | `hive` | `` `…` `` | `'…'` | lower |
| `mariadb` | MariaDB | `mysql` | `` `…` `` | `'…'` | upper |
| `materialize` | Materialize | `postgres` | `"…"` | `'…'` | lower |
| `mysql` | MySQL | `ansi` | `` `…` `` | `'…'` | upper |
| `oracle` | Oracle | `ansi` | `"…"` | `'…'` | upper |
| `postgres` | PostgreSQL | `ansi` | `"…"` | `'…'` | lower |
| `redshift` | Amazon Redshift | `postgres` | `"…"` | `'…'` | lower |
| `snowflake` | Snowflake | `ansi` | `"…"` | `'…'` | upper |
| `sparksql` | Apache Spark SQL | `ansi` | `` `…` `` | `'…'` | upper |
| `sqlite` | SQLite | `ansi` | `"…"`, `[ … ]` | `'…'` | upper |
| `starrocks` | StarRocks | `mysql` | `` `…` `` | `'…'` | upper |
| `teradata` | Teradata | `ansi` | `"…"` | `'…'` | upper |
| `trino` | Trino | `ansi` | `"…"` | `'…'` | upper |
| `tsql` | Microsoft T-SQL | `ansi` | `[ … ]`, `"…"` | `'…'` | upper |
| `vertica` | Vertica | `ansi` | `"…"` | `'…'` | upper |

"Unquoted ident case" is the fold applied when matching unquoted identifiers
case-insensitively — it is what makes `Foo` and `foo` the same name on
`postgres` but distinct on `ansi` unless the engine folds too. It feeds
`CP02`, `AL04`, `AL08`, and `RF06`. **[decision]**

> **Quoting is contextual, not absolute.** On `mysql`, `"…"` is a *string
> literal* by default and only an identifier under `ANSI_QUOTES`; on `hive` and
> `bigquery`, `"…"` is likewise a literal. The lexer records the delimiter and
> the grammar decides the role. `RF05`/`RF06` therefore consult the dialect's
> quoting map rather than assuming ANSI.

---

## 3. Inheritance tree

Overrides flow down and are inherited: a descendant reselects the parent's
grammar and changes only the deltas. This is why `redshift` is a small file —
it is `postgres` plus a fixed set of Redshift-only constructs.

```
ansi  (base — the default)
├── athena
├── bigquery
├── clickhouse
├── db2
├── exasol
├── flink
├── hive
│   └── impala
├── mysql
│   ├── doris
│   ├── mariadb
│   └── starrocks
├── oracle
├── postgres
│   ├── duckdb
│   ├── greenplum
│   ├── materialize
│   └── redshift
├── snowflake
├── sparksql
│   └── databricks
├── sqlite
├── teradata
├── trino
├── tsql
└── vertica
```

Dialects that are structurally closest to a sibling inherit from it; the rest
extend ANSI directly. **[decision]**

---

## 4. Rules and dialects

Rule applicability is defined in [`RULES.md`](./RULES.md) §1.6 and §10; this
section only records the dialect-side facts that drive it.

- **`all` dialects.** Most baseline rules (`AL`–`ST`, `LT`, `CP`, `CV`) apply
  everywhere and are skipped only where a construct makes them meaningless.
- **Skipped rules** (skipped, never errored):
  - `AM02` (`DISTINCT` vs `UNION`) — skipped on `duckdb`, `exasol`, `postgres`,
    `sparksql`, where `UNION DISTINCT` is not distinct from `UNION`.
  - `CV01` (`!=` vs `<>`) — skipped on `teradata`.
  - `CV11` (casting style) — skipped on `athena`, `teradata`, `trino`.
- **Disabled by default** (present but off unless `--select` or config enables
  it), because the dialect permits dynamic references that the rule would
  otherwise flag:
  - `RF01` (`references.from`) — off by default on `athena`, `bigquery`,
    `databricks`, `duckdb`, `hive`, `redshift`, `sparksql`.
  - `RF03` (qualification consistency) — off by default on `athena`,
    `bigquery`, `hive`, `redshift`.
- **Dialect families** (`RULES.md` §10): `PG` (PostgreSQL/Greenplum/Materialize),
  `OR` (Oracle), `TQ` (T-SQL). Each carries `force_enable` so it can be turned
  on outside its home dialect when the user asks.
- **Reserved extension families** (`RULES.md` §10.4): `MY`, `BQ`, `SF`, `RS`,
  `CH`, `TD`, `TR`, `DG`, `SP` — the next tranche of dialect rules, mapped to
  `mysql`, `bigquery`, `snowflake`, `redshift`, `clickhouse`, `teradata`,
  `trino`, `databricks`, `sparksql`.

---

## 5. Quoting and casing model

The lexer and the two `CP`/`RF` rule groups share one quoting model, so they can
never disagree.

- **Lexer matchers.** Each dialect declares its identifier, literal, and comment
  matchers. Differences are small: `mysql`/`doris`/`starrocks`/`hive` use a
  backtick identifier; `sqlite` adds `[… ]`; `tsql` adds `[… ]`; `bigquery`
  adds backticks and treats `"…"` as a literal; `hive` and `impala` treat `"…"`
  as a literal.
- **Case folding.** Unquoted identifiers fold to the dialect's case for
  case-insensitive comparison; quoted identifiers are always case-sensitive
  (`AL05`'s `alias_case_check = quoted_cs_naked_ci` and kindred policies).
- **`CP02`** infers the capitalisation of unquoted identifiers from the dialect
  default and the file's own usage; `CP03`/`CP05` do the same for function and
  type names.
- **`RF05`/`RF06`** warn on special characters and unnecessary quoting using the
  dialect's quote map, so a name that *must* be quoted on `postgres` is not
  flagged as merely unnecessary on `mysql`.

---

## 6. `target-version` gating

A rule that depends on a construct introduced in a specific engine version runs
only when the target version warrants it (SPEC §8.2). `target-version` is
interpreted **per dialect**; absent a value, only version-independent rules run.
**[decision]**

| Dialect | Value shape | Example gates |
|---|---|---|
| `postgres` | major number (`"13"`, `"16"`) | `MERGE` (15+), `JSON` (9.4+), `LATERAL` (9.3+), generated columns (12+) |
| `mysql` / `mariadb` | `"8.0"`, `"5.7"` | CTEs, window functions, `JSON_TABLE` (8.0) |
| `tsql` | year (`"2016"`, `"2019"`, `"2022"`) | `STRING_AGG` (2017+), `GENERATE_SERIES` (2022+) |
| `oracle` | release (`"11"`, `"19"`, `"23"`) | `FETCH FIRST` (12+), `QUALIFY` (23) |
| `bigquery` | release year | `QUALIFY`, `PIVOT`, `MATCH_RECOGNIZE` |
| `snowflake` | feature flags | constructs gated by feature name, not a number |
| `sparksql` / `databricks` | `"3.x"` | `QUALIFY`, `LATERAL` join forms |
| `sqlite` | `"3.x"` | window functions (3.25+), `RETURNING` (3.35+) |
| `redshift` | numeric | `SUPER`, `MATERIALIZED VIEW` refresh modes |

Version gating is expressed on the rule (`RULES.md` §1.6), not in the grammar:
the grammar stays permissive so the parse never fails on a newer construct, and
the rule simply declines to fire when the target version does not include it.

---

## 7. Per-dialect notes

Each note lists the parent, the quoting and casing deltas, the constructs the
grammar must accept, and any rule interaction. "Must accept" means the parser
tolerates the construct even where no rule targets it — breadth of *parsing* is
separate from breadth of *linting*.

### 7.1 `ansi`

The base. `''` strings, `"…"` identifiers, `UPPERCASE` default casing. On the
order of two hundred segment types, covering `SELECT`, DML, DDL, `WITH`, set
operations, window functions, `CASE`, and the standard expression grammar. Every
other dialect is this plus deltas; the largest dialects (BigQuery, Databricks)
push past three hundred segment types. Expects `;` statement terminators
(`CV06`) and a single trailing newline (`LT12`).

### 7.2 `athena`

Parent `ansi`. Lowercases unquoted identifiers; accepts backtick and double-quote
identifiers, plus the Hive/Trino-style `ARRAY`/`MAP`/`STRUCT` types, `UNNEST`,
`LATERAL VIEW`, `TABLESAMPLE`, and partition/serde DDL. `CV11` skipped; `RF01`
and `RF03` off by default.

### 7.3 `bigquery`

Parent `ansi`. Backtick identifiers; `"…"` is a literal. Additions include
`STRUCT`/`ARRAY`, `UNNEST`, `QUALIFY`, `EXCEPT`/`REPLACE` in `SELECT *`,
`SAFE.`-prefixed functions, scripting (`BEGIN…END`, `DECLARE`, `SET`), and
`MATCH_RECOGNIZE`. `RF01` and `RF03` off by default.

### 7.4 `clickhouse`

Parent `ansi`. Backtick or double-quote identifiers. Additions include `FINAL`,
`PREWHERE`, `ARRAY JOIN`, `SETTINGS`, `SAMPLE`, table engines (`MergeTree`
family), and `AS` aliases with `exceptions`. Extension family `CH` is reserved.

### 7.5 `databricks`

Parent `sparksql`. Delta-Lake statements (`MERGE`, `OPTIMIZE`, `VACUUM`,
`COPY INTO`), `QUALIFY`, `LATERAL` joins, `TABLESAMPLE`, and `GENERATE_SERIES`.
`RF01` off by default. Extension family `DG` is reserved.

### 7.6 `db2`

Parent `ansi`. Standard DB2 DDL/DML, `MERGE`, `FINAL TABLE`, `ORDER BY` in
subselects, and DB2 isolation syntax.

### 7.7 `doris`

Parent `mysql`. Backtick identifiers, MySQL-compatible DML, plus Doris
`DISTRIBUTED BY`/`PROPERTIES`/`BUCKETS` table options and `AGAINST` full-text
clauses.

### 7.8 `duckdb`

Parent `postgres`. Lowercases unquoted identifiers. Additions include
`FROM`-first `SELECT`, `PIVOT`/`UNPIVOT`, `QUALIFY`, `EXCLUDE`/`REPLACE` in
`SELECT *`, `ASOF` joins, list/struct types, and `READ_CSV`/`READ_PARQUET`
table functions. `AM02` skipped; `RF01` off by default.

### 7.9 `exasol`

Parent `ansi`. Exasol DDL, `CONNECT BY`, `PRIOR`, analytic functions, and
`IMPORT`/`EXPORT` statements. `AM02` skipped.

### 7.10 `flink`

Parent `ansi`. Flink SQL streaming: `WATERMARK`, `KV`/`TEMPORAL` joins,
`TUMBLE`/`HOP`/`CUMULATE` windowing TVFs, and `CREATE TABLE … WITH (…)`.

### 7.11 `greenplum`

Parent `postgres`. Essentially PostgreSQL with Greenplum `DISTRIBUTED BY`/
`DISTRIBUTED RANDOMLY`, `APPEND`/`AO`/`CO` storage options, and external tables.
Rule family `PG` applies.

### 7.12 `hive`

Parent `ansi`. Lowercases unquoted identifiers; backtick identifiers; `"…"` is a
literal. `LATERAL VIEW`, `DISTRIBUTE BY`/`SORT BY`/`CLUSTER BY`, partition and
serde DDL, `INSERT OVERWRITE`, and `STRUCT`/`ARRAY`/`MAP` types. `RF01` and
`RF03` off by default.

### 7.13 `impala`

Parent `hive`. Lowercases unquoted identifiers. Impala-specific `COMPUTE
STATS`/`INVALIDATE METADATA`, `KUDU` table options, and `UPSERT`.

### 7.14 `mariadb`

Parent `mysql`. MariaDB deltas: `RETURNING` on `INSERT`/`REPLACE`, `SEQUENCE`,
`SYSTEM-VERSIONED`/`APPLICATION-TIME PERIOD` tables, and `JSON_TABLE`.

### 7.15 `materialize`

Parent `postgres`. Streaming materialized views, `SUBSCRIBE`, `TAIL`, `CLUSTER`
and source/sink DDL, and `SINK`/`SOURCE` statements. Family `PG` applies.

### 7.16 `mysql`

Parent `ansi`. Backtick identifiers; `"…"` is a literal by default. Additions
include `LIMIT n, m`, `REPLACE INTO`, `INSERT … ON DUPLICATE KEY UPDATE`,
`GROUP_CONCAT`, `JSON_TABLE`, `WITH RECURSIVE`, and index/`ENGINE`/`CHARSET`
table options. Extension family `MY` is reserved.

### 7.17 `oracle`

Parent `ansi`. Double-quote identifiers; `UPPERCASE` folding. Additions include
hierarchical `CONNECT BY`/`START WITH`, the legacy `(+)` outer-join operator,
`ROWNUM`, `DUAL`, `MERGE`, `FETCH FIRST`, `MODEL`, and PL/SQL blocks. Carries
the baseline rule family `OR` (e.g. `OR01 oracle.empty_batch`).

### 7.18 `postgres`

Parent `ansi`. Double-quote identifiers; unquoted identifiers fold to lower.
Additions include `::` casts, `RETURNING`, `ON CONFLICT`, `LATERAL`,
`DISTINCT ON`, `ILIKE`, `SIMILAR TO`, `ARRAY`/`JSONB`/`JSONPATH` operators,
`FILTER` aggregates, `GENERATED … AS IDENTITY`, and `COPY`. Carries baseline
family `PG` (`PG01` locks, `PG02` `NOT VALID` foreign keys).

### 7.19 `redshift`

Parent `postgres`. `DISTKEY`/`SORTKEY`/`DISTSTYLE`, `SUPER` type and PartiQL
navigation, `COPY`/`UNLOAD`, `SPECTRUM` external tables, and `MATERIALIZED VIEW`
refresh. Lowercase folding. `RF01` and `RF03` off by default; extension family
`RS` is reserved.

### 7.20 `snowflake`

Parent `ansi`. Double-quote identifiers; unquoted identifiers fold to upper.
Additions include `QUALIFY`, `SAMPLE`, `FLATTEN`, `VARIANT`/`OBJECT`/
`ARRAY`, `MERGE`, `PIVOT`/`UNPIVOT`, staged `COPY INTO`, `TASK`/`STREAM`
DDL, `::` casts, and semi-structured `:` traversal. Extension family `SF` is
reserved.

### 7.21 `sparksql`

Parent `ansi`. Backtick identifiers; `UPPERCASE` folding. Additions include
`QUALIFY`, `LATERAL VIEW`, `TABLESAMPLE`, Delta/Hive-compatible DDL,
`GENERATE_SERIES`, and `STRUCT`/`ARRAY`/`MAP`. `AM02` skipped; `RF01` off by
default.

### 7.22 `sqlite`

Parent `ansi`. `"…"` and `[… ]` identifiers; `'…'` strings. Additions include
`INSERT OR REPLACE`/`OR IGNORE`, `AUTOINCREMENT`, `WITHOUT ROWID`, `RETURNING`
(3.35+), `UPSERT`, and dynamic typing in column definitions.

### 7.23 `starrocks`

Parent `mysql`. Backtick identifiers. MySQL-compatible DML plus StarRocks
`DISTRIBUTED BY`, `PROPERTIES`, `MATERIALIZED VIEW`, and bitmap/HLL aggregates.
Extension family `RS`-adjacent (StarRocks uses MySQL family conventions).

### 7.24 `teradata`

Parent `ansi`. Double-quote identifiers. Additions include `SEL` shorthand,
`QUALIFY`, `SAMPLE`, `HELP`/`SHOW` statements, and `BTEQ`-style batch syntax.
`CV01` and `CV11` skipped; extension family `TD` is reserved.

### 7.25 `trino`

Parent `ansi`. Double-quote identifiers. Additions include `UNNEST` with
`WITH ORDINALITY`, `CROSS JOIN UNNEST`, `LATERAL`, `TABLESAMPLE`, `MATCH_RECOGNIZE`,
`ROW`/`ARRAY`/`MAP`, and `LAMBDA` expressions. `CV11` skipped; extension family
`TR` is reserved.

### 7.26 `tsql`

Parent `ansi`. `[… ]` and `"…"` identifiers; `GO` batch separator; `UPPERCASE`
folding. Additions include `TOP`, `PIVOT`/`UNPIVOT`, `MERGE`, `OUTPUT`,
`TRY…CATCH`, stored-procedure and `BEGIN…END` batch syntax, temp tables, and
`NOLOCK` hints. Carries baseline family `TQ` (`TQ01`–`TQ04`).

### 7.27 `vertica`

Parent `ansi`. Double-quote identifiers. Additions include `COPY`/`EXPORT`,
projections, `SEGMENTED BY`, `KSAFE`, flex tables, and Vertica-specific
`PARTITION BY` DDL. Extension family `SP`-adjacent (Vertica uses ANSI
conventions).

---

## 8. Adding a dialect

Adding a dialect is a bounded, testable task:

1. **Declare** a new file in `internal/dialect/` that `copy_as` the nearest
   ancestor (`ansi` unless a kindred dialect exists).
2. **Override** only the segments that differ; replace the identifier/literal/
   comment lexer matchers only when quoting actually differs.
3. **Extend** the keyword and reserved-word sets.
4. **Record** rule deltas: `skip` and `disabled-by-default` lists, and any new
   dialect-family rules.
5. **Register** it in the dialect registry (lazy; never imported by the core).
6. **Test** it: a parse-fixture directory of real-world `.sql` files that must
   round-trip (`Unparse(Parse(src)) == src`, SPEC §13), plus at least one fixture
   exercising every overridden segment.

A dialect is **stable** when its fixtures round-trip on the full corpus and its
rule deltas are asserted by fixtures. **[decision]**

---

## 9. Testing dialects

- `testdata/dialects/<dialect>/*.sql` — parse fixtures; each must round-trip
  byte-for-byte and produce no unexpected parse errors.
- `testdata/dialects/<dialect>/<case>.expected` — optional expected tree or
  diagnostics for constructs that also carry a rule.
- A property test asserts losslessness for every fixture across every dialect.
- A registry test asserts the documented parent graph (§3) matches the code —
  an accidental re-parent fails CI.
- A coverage report `rusq dialects` prints each dialect's segment count and the
  rules it skips, so the §2 table is verifiable from the binary.

---

## 10. Open questions

- **OQ-1** *Exotic dialects.* Does SOQL (and other non-relational or niche
  dialects) join the set, and on what cadence? The registry is extensible; the
  question is product priority, not feasibility. (Mirrors `PRD.md` OQ-4.)
- **OQ-2** *`target-version` for feature-flagged dialects.* Snowflake gates on
  feature names rather than numbers; the encoding of that in config is `[open]`
  (SPEC §12.8).
- **OQ-3** *Shared keyword sets.* Whether `mysql`-derived dialects share one
  keyword module or each keeps its own, trading duplication against churn
  isolation — a build-time decision with no user-visible surface.
- **OQ-4** *Dialect-version matrix fixtures.* Whether to ship a fixture per
  (dialect, target-version) pair or gate by construct, given the combinatorial
  size.