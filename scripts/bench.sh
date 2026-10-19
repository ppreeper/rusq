#!/usr/bin/env bash
#
# Micro-benchmark runner for rusq. See docs/PERFORMANCE.md §5.
#
#   scripts/bench.sh [PATTERN] [BENCHTIME]
#
# Every run is written to bench/<sha>-<utc>.txt. When benchstat is installed and
# an earlier run exists, a comparison against it is printed too.
#
# Environment:
#   BENCHTIME   per-benchmark time limit  (default 200ms)
#   BENCH_COUNT repetitions per benchmark (default 1; use 6+ for benchstat)
set -euo pipefail

pattern="${1:-.}"
benchtime="${2:-${BENCHTIME:-200ms}}"
count="${BENCH_COUNT:-1}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

sha="$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out="bench/${sha}-${stamp}.txt"
mkdir -p bench

echo "rusq: $(go version)"
echo "rusq: pattern=${pattern} benchtime=${benchtime} count=${count}"
echo "rusq: writing ${out}"

go test -run '^$' -bench "${pattern}" -benchmem -benchtime "${benchtime}" -count "${count}" ./internal/... | tee "${out}"

if command -v benchstat >/dev/null 2>&1; then
	prev="$(ls -1t bench/*.txt 2>/dev/null | grep -v "/$(basename "${out}")$" | head -1 || true)"
	if [ -n "${prev}" ]; then
		echo
		echo "rusq: benchstat ${prev} ${out}"
		benchstat "${prev}" "${out}"
	fi
else
	echo "rusq: benchstat not installed (go install golang.org/x/perf/cmd/benchstat@latest)"
fi
