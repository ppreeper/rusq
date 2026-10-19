#!/usr/bin/env bash
#
# Deterministic corpus generator for rusq. See docs/PERFORMANCE.md §6.
#
#   scripts/gen-corpus.sh [DIR] [FILES]
#
# Files land in DIR/<dialect>/f000000.sql so a dialect can be measured on its
# own. The index -> dialect/template mapping is fixed, so the same FILE count
# always produces byte-identical content; nothing here is random.
set -euo pipefail

dir="${1:-/tmp/rusq-corpus}"
n="${2:-20000}"

DIALECTS=(
	ansi athena bigquery clickhouse databricks db2 doris duckdb
	exasol flink greenplum hive impala mariadb materialize mysql
	oracle postgres redshift snowflake sparksql sqlite starrocks
	teradata trino tsql vertica
)
ndialects="${#DIALECTS[@]}"

mkdir -p "$dir"
manifest="$dir/MANIFEST.tsv"
: > "$manifest"

for ((i = 0; i < n; i++)); do
	d="${DIALECTS[$((i % ndialects))]}"
	t=$((i % 4))
	sub="$dir/$d"
	mkdir -p "$sub"
	f="$(printf '%s/f%06d.sql' "$sub" "$i")"

	case "$t" in
	0)
		printf 'SELECT id, name, created_at\nFROM t%d\nWHERE status = %d AND created_at > CURRENT_DATE - INTERVAL 30 DAY\nORDER BY created_at DESC\nLIMIT %d;\n' \
			"$i" "$((i % 7))" "$((i % 100 + 1))" > "$f"
		;;
	1)
		printf 'WITH recent AS (\n    SELECT customer_id, SUM(total) AS revenue\n    FROM orders_%d\n    WHERE created_at >= CURRENT_DATE - INTERVAL 7 DAY\n    GROUP BY customer_id\n)\nSELECT c.id, c.name, r.revenue\nFROM customers_%d AS c\nLEFT JOIN recent AS r ON r.customer_id = c.id\nWHERE c.active = TRUE\nORDER BY r.revenue DESC;\n' \
			"$i" "$i" > "$f"
		;;
	2)
		printf 'INSERT INTO audit_%d (id, actor, at, note)\nVALUES (%d, %d, CURRENT_TIMESTAMP, %d);\n' \
			"$i" "$i" "$((i % 1000))" "$((i % 13))" > "$f"
		;;
	3)
		printf 'CREATE TABLE fact_%d (\n    id INTEGER NOT NULL,\n    amount NUMERIC(12, 2),\n    happened_at TIMESTAMP,\n    PRIMARY KEY (id)\n);\n' \
			"$i" > "$f"
		;;
	esac

	printf '%s\t%s\t%s\n' "$d/f$(printf '%06d' "$i").sql" "$d" "$t" >> "$manifest"
done

echo "rusq: wrote $n files under $dir ($ndialects dialects)"
echo "rusq: manifest $manifest"
