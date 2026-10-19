#!/usr/bin/env bash
# bench-gate.sh — compare benchmark results against a stored baseline and fail
# when a benchmark regresses by more than a threshold.
#
#   scripts/bench-gate.sh [BASELINE] [THRESHOLD_PERCENT]
#
# BASELINE defaults to the newest file in bench/ (see scripts/bench.sh). When no
# baseline exists the current run is archived and the gate passes, which is how
# the first run on a branch bootstraps. THRESHOLD defaults to BENCH_THRESHOLD or
# 5 (percent).
#
# Regression is measured on the median ns/op of each benchmark, so it is a
# relative gate: it compares the branch against a baseline recorded on the same
# class of machine, never against an absolute ceiling (see PERFORMANCE.md §9).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

baseline="${1:-}"
threshold="${2:-${BENCH_THRESHOLD:-5}}"
pattern="${BENCH_PATTERN:-.}"
count="${BENCH_COUNT:-5}"
benchtime="${BENCHTIME:-200ms}"

if [[ -z "$baseline" ]]; then
	baseline="$(ls -1t bench/*.txt 2>/dev/null | head -n1 || true)"
fi

tmp="$(mktemp -t rusq-bench-XXXXXX.txt)"
out="bench/$(git rev-parse --short HEAD 2>/dev/null || echo nogit)-$(date -u +%Y%m%dT%H%M%SZ).txt"

echo "==> benchmarks: pattern=${pattern} benchtime=${benchtime} count=${count}"
go test -run '^$' -bench "$pattern" -benchmem -benchtime "$benchtime" -count "$count" ./internal/... >"$tmp" 2>&1 || {
	cat "$tmp" >&2
	exit 2
}
mkdir -p bench
cp "$tmp" "$out"
echo "==> archived run: $out"

if [[ -z "$baseline" || ! -f "$baseline" ]]; then
	echo "==> no baseline to compare against; recording $out as the baseline"
	exit 0
fi
echo "==> baseline: $baseline (threshold ${threshold}%)"

python3 - "$baseline" "$out" "$threshold" <<'PY'
import re, sys, statistics

base_path, new_path, threshold = sys.argv[1], sys.argv[2], float(sys.argv[3])
line = re.compile(r'^(Benchmark\S+)(?:/\S+)?(?:-\d+)?\s+\d+\s+([\d.]+)\s+ns/op')


def read(path):
    out = {}
    for raw in open(path):
        m = line.match(raw.strip())
        if not m:
            continue
        out.setdefault(m.group(1), []).append(float(m.group(2)))
    return {k: statistics.median(v) for k, v in out.items()}


base, new = read(base_path), read(new_path)
if not base or not new:
    print("!! could not parse both runs; skipping the gate", file=sys.stderr)
    sys.exit(0)

rows, regressions = [], 0
for name in sorted(new):
    if name not in base:
        rows.append(f"  new      {name:<44} {new[name]:10.1f} ns/op")
        continue
    delta = (new[name] - base[name]) / base[name] * 100.0
    flag = ""
    if delta > threshold:
        flag = "REGRESSION"
        regressions += 1
    rows.append(
        f"  {delta:+6.1f}%  {name:<44} {base[name]:10.1f} -> {new[name]:10.1f} ns/op  {flag}"
    )

print("==> benchmark comparison (median ns/op, lower is better)")
print("\n".join(rows))
if regressions:
    print(f"!! {regressions} benchmark(s) regressed by more than {threshold}%", file=sys.stderr)
    sys.exit(1)
print(f"==> no benchmark regressed by more than {threshold}%")
PY
