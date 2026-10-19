# rusq — Performance

> Speed is not a nice-to-have here: it is the reason the project exists. This
> document records what "fast" means, the budgets each stage must meet, how the
> numbers are produced, and which design decisions buy the speed.

Status: **draft v0.1** — companion to [`SPEC.md`](./SPEC.md) §11–12 and
[`PRD.md`](./PRD.md) §9. No public measurements exist yet: this is a
pre-implementation budget, not a results page. When the benchmark harness
(§5–§7) lands, measured tables are added here and the targets below become the
regression contract. `[decision]` marks a recorded choice; `[open]` marks
something still being settled.

Companion documents:

- [`README.md`](../README.md) — what `rusq` is and how to run it.
- [`PRD.md`](./PRD.md) — product requirements, including the `NFR-PERF-*` targets.
- [`SPEC.md`](./SPEC.md) — the technical specification (§11 caching, §12
  architecture and concurrency).
- [`RULES.md`](./RULES.md) — the rule catalog; the default set is deliberately
  small, which is also a performance decision.
- [`DIALECTS.md`](./DIALECTS.md) — dialect gating, which lets rules decline to
  run rather than lex constructs that cannot occur.
- [`TEMPLATING.md`](./TEMPLATING.md) — rendering and variant limits, which bound
  the cost of templated files.
- [`EDITOR.md`](./EDITOR.md) — the editor path, where per-file latency matters
  most.

---

## 1. Performance is a requirement

The project's premise is **the same breadth of coverage as the mature Python
reference, at native speed, with no interpreter**. That makes performance a
product requirement, not an optimisation pass:

- **Latency is a feature.** A linter that takes a second per file is not run on
  save, and a linter that is not run on save does not catch the bug before the
  warehouse does.
- **Correctness still gates.** No budget below is worth a false negative. When
  a faster path and a correct path disagree, the correct path wins and the
  faster one is fixed.
- **Budgets are contracts.** Each stage has an allocation and time budget
  (§3). A change that blows a budget needs a benchmark in the PR explaining
  why, not a shrug.
- **Determinism is part of the contract.** Parallel and serial runs must emit
  byte-identical output (`NFR-COR-3`, `NFR-PERF-3`); a speedup that reorders
  output is a regression.

---

## 2. Targets and budgets

Targets are for a modern x86-64 laptop-class machine (8 physical cores) on a
quiet system, with a warm OS file cache unless "cold" is stated. "Cold" here
means **`rusq`'s own cache is empty**, not that the operating system's page
cache has been dropped — that distinction is called out wherever it matters.

| ID | Metric | Target | Guarded by |
|---|---|---|---|
| P-1 | Cold run, 20k-file project, 8 workers | **< 5 s** | `BenchmarkCheckFiles`, corpus sweep |
| P-2 | Warm run, same project (cache hit) | **< 1 s** | cache-hit corpus sweep |
| P-3 | Single-file lint, median, typical model (< 300 lines) | **< 5 ms** | `BenchmarkLint` |
| P-4 | Single-file lint, p95 | **< 15 ms** | corpus percentile report |
| P-5 | Scaling efficiency, 1 → 8 workers | **≥ 70%** of linear | scaling sweep |
| P-6 | Process start to first file opened | **< 20 ms** | `BenchmarkStartup` |
| P-7 | Peak RSS, 20k-file sweep | **< 750 MB** | corpus sweep (`/usr/bin/time -v`) |
| P-8 | Steady-state per-worker working set | **< 32 MB** | profile + corpus |
| P-9 | Cache hit path, per file | **< 1 ms** | `BenchmarkCacheHit` |
| P-10 | Formatter, already-formatted file | **< 1.5× lint cost** | `BenchmarkFormatIdempotent` |
| P-11 | Release binary, stripped, `linux/amd64` | **< 30 MB** | release job |
| P-12 | Lint throughput, single worker | **> 400 files/s** typical | corpus sweep |

P-1 and P-3 are the product-level numbers; the rest exist so that a regression
can be localised before it shows up as a slower end-to-end run.

The arithmetic behind P-1: 20,000 files in 5 s wall on 8 workers is ~4,000
files/s wall, or ~500 files/s per worker, or a ~2 ms average wall cost per file
per worker. P-3's 5 ms median is therefore a *single-file, single-worker*
budget with room for a slower tail, not a contradiction.

---

## 3. The per-file cost model

A typical model is ~300 lines / ~12 KB of SQL. The budget below is for one lint
pass on such a file with the default rule set, on one worker. Formatting is
budgeted separately because it is never on the lint path.

| Stage | Budget | Notes |
|---|---|---|
| Read + hash | 60 µs | `read(2)` plus BLAKE2b-256 over the bytes |
| Templating | 150 µs | `raw` is a no-op; this is the placeholder budget |
| Lex | 400 µs | one forward byte scan, no regex |
| Parse | 1,400 µs | builds the lossless CST and source index |
| Lint (default set, 22 rules) | 2,000 µs | cheapest-first passes; most files touch token/line rules only |
| Merge + diagnostics | 150 µs | per-file; the global sort happens once |
| **Total (lint)** | **~4.2 ms** | under the 5 ms median target |
| Fix pass (when `--fix`) | +1,500 µs | bounded iterations, usually one |
| Format (when `format`) | +1,800 µs | IR construction plus printing |
| Round-trip check (`Unparse`) | ~40 µs | re-emits source spans; O(1) allocations |

The budgets are deliberately round numbers with headroom. They are also
*allocation* budgets: the hot path targets under 4,000 allocations per typical
file, and `Unparse` at **one** allocation regardless of document size.

---

## 4. Where the wins come from

Every decision below is in [`SPEC.md`](./SPEC.md); this section records **why**
it matters for the numbers.

### 4.1 One parse, two views

The lexer and parser run once. The canonical tree is the **lossless CST**; the
typed AST is a lazy projection over it (§12.2 of the spec). Token-tier rules
never materialise the AST, and structural rules never trigger a second parse.
This is the single largest saving: parse cost is paid once per file, and rule
tiers are selected by what they actually need.

### 4.2 Cheapest-first pass schedule

Rules are grouped by source (`Token`, `Lines`, `Tree`, `File`, `Index`) and run
in that order. A token rule that fires on a file does not cost a tree walk that
was never needed. The default set is small (§4.9), so most files finish in the
token and line passes.

### 4.3 A byte-oriented lexer with no regex on the hot path

The lexer is a single forward scan over `[]byte` with a keyword trie and
switch-based matchers for identifiers, strings, and comments. Regular
expressions are permitted in *cold* paths (config validation, ignore-file
parsing) and in rule options that are explicitly regex-shaped — never per token
and never per line in a default-on rule.

### 4.4 `Unparse` re-emits source spans

The CST stores byte offsets into the original source. Round-tripping returns
the untouched spans directly rather than rebuilding text from nodes, so
`Unparse` allocates once. This is what makes the round-trip property test cheap
enough to run over the whole corpus, and it is what the fix engine and formatter
rely on for byte-exact edits.

### 4.5 Lazy typed AST

The typed wrappers around CST nodes (selects, joins, column references, CTEs)
are built on demand by the rules that ask for them and are not retained after
the file is linted. The AST is a view, never a second representation that must
be kept in sync.

### 4.6 Bounded worker pool with per-file isolation

Work is parallelised over files, not over rules within a file. Each file is an
independent unit with no shared mutable state; shared services (resolved
settings, the cache handle, the cross-file index) are read-mostly and
synchronised once. The only serial section is the final merge and sort, which
is O(n log n) over diagnostics and negligible for clean runs.

This is also the fix for the classic scaling bug: per-worker caches that
rebuild the same index are worse than no cache at all. Shared state lives in one
`runState` with memoised lookups (§4.7), so N workers do one build.

### 4.7 Config discovery is memoised per directory

Walking a large tree touches the same directories repeatedly. Config discovery
(closest-wins, `extend`, XDG fallback) is resolved once per directory and
memoised for the run; per-file effective settings are deduplicated so rules see
an interned settings value rather than a fresh struct per file.

### 4.8 Content-addressed caching

The cache key is a BLAKE2b-256 of the file bytes **plus** a settings hash
(dialect, templater, selections, rule options, tool version). The hit path is
stat → read → hash → map lookup. It does not parse, lex, or lint. A settings
change invalidates the whole run by construction, which is what makes a warm
run safe as well as fast.

The hash is over content, not mtime: a touched-but-unchanged file still hits,
and a changed file with an old mtime still misses. `--no-cache` and
`rusq clean` bypass and clear it for benchmarking.

### 4.9 Do not do work that was not asked for

Three deliberate choices keep the common case cheap:

- **Small default set.** Only correctness, suspicious, and security rules are
  on by default (22 rules including the two meta rules). Style and formatting
  rules are opt-in. A default lint is not the `ALL` run.
- **Dialect and version gating.** A rule that cannot apply to the configured
  dialect or target version declines before it touches the tree.
- **No formatting on the lint path.** `check` never constructs the formatter's
  IR. `format --check` does, and that cost is reported separately.

### 4.10 Bounded templating

Templated files are the one case where a single file can produce more than one
candidate text. The number of render variants is capped
(`render-variant-limit`, default 5), diagnostics are deduplicated across
variants, and a file that cannot be rendered falls back to substitution rather
than retrying. Cost is bounded by construction, not by the shape of the
template.

### 4.11 Allocation discipline

The hot path avoids interface boxing in per-token loops, reuses scanner buffers
per worker, and builds slices with capacity where the size is known. Diagnostics
are appended to a per-worker slice and sorted once at merge. The benchmark
suite reports `allocs/op` and `B/op`, and the CI gate watches both, because an
allocation regression is usually a latency regression a few months later.

---

## 5. Benchmark harness

Benchmarks are ordinary Go benchmarks so they run with the standard toolchain
and no extra dependency.

```console
# Micro-benchmarks across the engine.
go test -run '^$' -bench . -benchmem ./internal/...

# One layer, longer runs, with profiles.
go test -run '^$' -bench 'BenchmarkParse' -benchtime=2s \
  -cpuprofile=cpu.out -memprofile=mem.out ./internal/parser

# Compare two revisions.
go test -run '^$' -bench . -count=6 ./internal/... > new.txt
benchstat old.txt new.txt
```

`make bench` runs the standard set (`BENCH_PATTERN`, `BENCHTIME`, `BENCH_COUNT`
override the defaults); `scripts/bench.sh` runs the same set, archives it under
`bench/<sha>-<utc>.txt`, and prints a `benchstat` comparison against the previous
run when `benchstat` is installed. `make corpus` and `scripts/gen-corpus.sh`
materialise the deterministic corpus described in §6, and
`make bench-corpus` times the built binary over it cold, warm, and with
`--workers 1`.

### 5.1 Conventions

- Names are `Benchmark<Layer>` with `/subcase` where the input differs:
  `BenchmarkParse/typical`, `BenchmarkParse/large`.
- Every benchmark calls `b.ReportAllocs()`; byte-oriented ones also call
  `b.SetBytes(int64(len(src)))` so throughput is reported in MB/s.
- Inputs are loaded once outside the timed loop and are immutable.
- A benchmark that measures a rule runs the rule in isolation; a benchmark that
  measures a pass runs the pass. End-to-end numbers come from the CLI
  benchmark, never from summing micro-benchmarks.

### 5.2 Benchmark catalog

| Benchmark | Package | Input | Guards |
|---|---|---|---|
| `BenchmarkStartup` | `internal/cli` | no files | P-6 |
| `BenchmarkWalk` | `internal/walk` | 20k paths | discovery cost |
| `BenchmarkConfigResolve` | `internal/config` | 1k nested dirs | §4.7 memoisation |
| `BenchmarkLex` | `internal/lexer` | 12 KB / 300 lines | scan throughput |
| `BenchmarkParse` | `internal/parser` | typical + 1 MB large | CST construction |
| `BenchmarkRoundTrip` | `internal/parser` | parsed tree | `Unparse` cost, allocations |
| `BenchmarkASTProject` | `internal/ast` | parsed tree | lazy projection |
| `BenchmarkLintDefault` | `internal/linter` | typical file, 22 rules | P-3 |
| `BenchmarkLintAll` | `internal/linter` | typical file, all rules | rule scaling |
| `BenchmarkLintLines` | `internal/linter` | 5k-line file | line-pass cost |
| `BenchmarkFix` | `internal/fix` | file with N safe fixes | iteration bound |
| `BenchmarkFormat` | `internal/formatter` | unformatted file | format cost |
| `BenchmarkFormatIdempotent` | `internal/formatter` | already formatted | P-10 |
| `BenchmarkCacheHit` | `internal/cache` | 12 KB entry | P-9 |
| `BenchmarkCheckFiles` | `internal/cli` | 50 files, end to end | P-3, P-12 |
| `BenchmarkCheckFilesCached` | `internal/cli` | 50 files, warm | P-2 path |
| `BenchmarkServerDidChange` | `internal/lsp` | one edit | editor latency |

Measured values are appended to this table when the harness lands. The table
is the contract: a new benchmark is added with the feature that needs it, not
after a regression.

---

## 6. End-to-end corpus methodology

Micro-benchmarks do not capture discovery, config resolution, IO, and merge
together. The end-to-end number is measured on a **corpus**, not on a folder of
`SELECT 1`.

### 6.1 The corpus

`testdata/corpus/` holds a checked-in sample of real-world-shaped SQL, and
`scripts/gen-corpus.sh` deterministically generates the 20k-file sweep from a
fixed seed: all 27 dialects, a mix of raw and templated files, a realistic
long tail of small models plus a handful of multi-thousand-line files. The
generator is versioned so the corpus changes only when we intend it to.

### 6.2 Protocol

1. Build with `go build ./cmd/rusq`.
2. Clear rusq's cache (`rusq clean`) for a cold run; leave it for a warm run.
3. Run with `--no-cache` when measuring pure engine throughput.
4. Report wall, user, sys, max RSS, and files/s.
5. Repeat at least three times; report the median, not the best run.
6. Run once with `--workers 1` and compare against the 8-worker run for P-5.
7. Diff the serial and parallel reports; they must be byte-identical after the
   documented ordering (path, then range).

```console
$ rusq clean
$ /usr/bin/time -v rusq check --no-cache testdata/corpus/ > /dev/null
$ /usr/bin/time -v rusq check --workers 1 --no-cache testdata/corpus/ > /dev/null
$ rusq check --no-cache --output-format json testdata/corpus/ | sha256sum
```

### 6.3 What is *not* claimed

Numbers are reported on a named machine with the Go version and commit SHA. We
do not claim a number is portable across machines, and we do not compare
against another tool's number measured on a different corpus. The only fair
comparison is the same machine, the same files, and the same rule selection.

---

## 7. Concurrency and scaling

The pool is `runtime.NumCPU()` workers by default, capped by `--workers`.
Files are claimed from a shared queue; results go to per-worker buffers and are
merged once.

| Workers | Wall target (relative) | Notes |
|---|---|---|
| 1 | 1.00× | the baseline; also the determinism reference |
| 2 | ~0.55× | |
| 4 | ~0.30× | |
| 8 | ~0.18× | ≥ 70% of linear (P-5) |
| 16 | ~0.15× | IO and merge begin to dominate |

Two rules keep scaling honest:

- **No per-worker full builds.** Shared services are built once and memoised;
  a worker that finds a memo miss builds it under a lock, not per file.
- **One serial merge.** Sorting and output are the only serial work, and the
  sort key is total (path, then range) so the result does not depend on
  scheduling.

The editor path (§4.4 of the editor doc) runs on one file and does not start a
pool; it reuses the warm cache and the in-process index if configured.

---

## 8. Memory

Peak RSS is dominated by three things: the worker count multiplied by the
per-file working set (source, CST, diagnostics), the cache index, and the
cross-file index when enabled.

| Component | Budget | Mechanism |
|---|---|---|
| Per worker, typical file | < 32 MB | source + CST + one AST projection |
| Cache index (20k files) | ~ 2–4 MB | one entry per file: hash, settings hash, diagnostics |
| Cross-file index | < 50 MB | only when `[index].enabled = true` |
| Templated file | bounded by `render-variant-limit` | variants are released after merge |

Nothing retains the corpus: files are read, linted, and dropped. The cache stores
diagnostics, not trees. A large file is the one case where the per-worker set
grows with input size, and the parser's depth/node limits (with a clear
diagnostic rather than an OOM) are the backstop.

---

## 9. Profiling and regression gates

### 9.1 Profiles

Every benchmark accepts the standard flags; the CLI exposes the same via
`RUSQ_PROFILE=cpu|mem|mutex|block` and `--profile-dir` for end-to-end runs.
`go tool pprof -http=:0 cpu.out` for the flame graph; `go test -trace` for
scheduler questions.

### 9.2 CI gates

- **Micro-benchmarks** run on every PR that touches `internal/lexer`,
  `internal/parser`, `internal/linter`, `internal/rules`, `internal/formatter`,
  or `internal/cache`.
- **`benchstat` compares** against the merge-base. A regression over **5%** in
  a P-* benchmark fails the job; between 5% and 10% warns.
- **The gate is scripted** as `scripts/bench-gate.sh` (make target
  `bench-gate`). It records a run into `bench/`, compares the **median `ns/op`**
  of each benchmark against a stored baseline, and exits non-zero when anything
  regresses by more than `BENCH_THRESHOLD` (default 5%). It does not require
  `benchstat` to be installed, so it runs anywhere Go does. **[decision]**
- **Allocation budgets** are checked as hard numbers per benchmark, because
  they move before latency does.
- **Corpus sweep** runs nightly on a dedicated runner and records wall, RSS,
  and files/s; a 10% regression opens an issue with the before/after profiles.

CI runners are noisy. Gates compare against the same runner and the merge-base,
never against an absolute number from this document; the absolute targets in §2
are release criteria, not per-PR gates. **[decision]** — there is no absolute
ceiling in the per-PR job: the gate is strictly relative (median vs baseline on
the same machine class), and the §2 numbers are verified by hand on a dedicated
runner before a release.

### 9.2.1 Platform coverage

`.github/workflows/ci.yml` runs `gofmt`, `go vet`, `go build`, `go test` and
(`ubuntu`/`macos` only) `go test -race` on **ubuntu, macOS, and Windows**, so
every code path that touches the filesystem, the terminal, or the process
environment is exercised on Windows even though §2's numbers describe Linux. The
absolute Windows IO and process-start figures remain unmeasured; they are to be
recorded on a Windows runner before v1.0 rather than assumed from the Linux
numbers. **[open]**

### 9.3 What a performance PR must include

A change to a hot package includes a benchmark, before/after `benchstat`
output, and one sentence on the mechanism. A change that trades correctness for
speed is rejected regardless of the number.

---

## 10. Deliberate non-optimisations

These are recorded so they are not re-litigated in every review:

- **No SIMD or assembly.** A straightforward byte loop in Go is fast enough for
  the targets; hand-written assembly is a maintenance cost the project does not
  need.
- **No parser shortcuts that change the grammar.** The grammar stays permissive
  and complete; speed comes from the pass schedule and allocation discipline,
  not from parsing less SQL.
- **No daemon required for speed.** The binary starts in tens of milliseconds;
  a resident daemon is an editor concern (the LSP), not a CLI requirement.
- **No network, ever.** Remote caching or a hosted index would trade the
  single-binary property for a number we do not need.
- **No whole-corpus in-memory index by default.** The cross-file index is opt-in
  because it is the only component whose memory grows with the project.

---

## 11. Reproducing results

```console
# Build the exact binary measured.
go build -trimpath -ldflags "-s -w" -o bin/rusq ./cmd/rusq

# Micro-benchmarks.
make bench

# End-to-end, cold and warm.
rusq clean
/usr/bin/time -v bin/rusq check --no-cache testdata/corpus/
/usr/bin/time -v bin/rusq check testdata/corpus/

# Scaling.
for w in 1 2 4 8; do
  /usr/bin/time -f "%e s  %M KB  workers=$w" \
    bin/rusq check --no-cache --workers "$w" testdata/corpus/ > /dev/null
done

# Determinism.
bin/rusq check --no-cache --workers 1 --output-format json testdata/corpus/ | sha256sum
bin/rusq check --no-cache --workers 8 --output-format json testdata/corpus/ | sha256sum
```

Report the machine (CPU model, cores, RAM), OS, Go version, commit SHA, and
whether the OS page cache was warm.

---

## 12. Open questions

- **OQ-P1 — Resolved: the per-PR gate is relative only.** `make bench-gate`
  compares median `ns/op` against a stored baseline on the same machine class and
  fails past `BENCH_THRESHOLD` (5%); there is no absolute ceiling in CI. The §2
  targets are enforced by hand on a dedicated runner at release time. **[decision]**
- **OQ-P2 — Resolved: a size cap with LRU eviction.** `[cache] max-size` accepts
  `512MB`, `2GiB`, `unlimited`, etc.; the default is **512 MiB**, and after each
  run the cache evicts its least recently modified entries until it fits
  (`Cache.Prune`), then drops directories left empty. Recency is mtime: an entry
  that keeps returning the same result is rewritten by `Put` only when it is
  stored, so untouched files age out first. Measured: a 2,000-file corpus writes
  about **42 bytes of JSON per clean file** (83 KB in total) and about **800
  bytes per file** when every file is full of findings (1.6 MB), so a 512 MiB
  cap holds hundreds of thousands of files — eviction exists for the pathological
  monorepo, not for everyday use. **[decision]**
- **OQ-P3** — Is `--workers` worth exposing, or should the pool always be
  `NumCPU`? It is useful for scaling benchmarks and for memory-constrained CI.
  **Resolved: `--workers <N>` is a global flag** (0 = `NumCPU`), used by
  `check`, the cross-file index build, and `make bench-corpus`. **[decision]**
- **OQ-P4 — Partly resolved: correctness on Windows is now covered, timings are
  not.** The CI matrix (§9.2.1) runs gofmt, vet, build, and tests on Windows as
  well as macOS and Linux, so the portable IO, terminal-detection, and
  process-start paths are continuously exercised. The absolute Windows numbers
  remain unmeasured and are deferred until a Windows runner is available for a
  timing sweep. **[open]**
- **OQ-P5 — Resolved: the index stays JSON.** Measured on a 2,000-file corpus
  (7.9 MB of SQL across all 27 dialects): `.rusq_cache/index.json` is **328 KB**
  — about 164 bytes per file, so ~3.3 MB for a 20,000-file project, and loading
  it is part of a 0.20 s cold (0.03 s warm) whole-corpus run. At that size a
  compact binary form would save a few hundred kilobytes and cost the
  inspectability that makes the store debuggable (it is plain JSON you can read,
  diff, and delete), so JSON is retained and revisited only if a real project
  makes the store large. **[decision]**
