// Package dialect describes the lexical and grammatical surface of each SQL
// dialect. Dialects inherit from the ANSI base and override only their deltas.
package dialect

import (
	"sort"
	"strings"
)

// Quote is a matched pair of identifier/string delimiters.
type Quote struct {
	Open  byte
	Close byte
}

// IdentifierCase records how an engine folds unquoted identifiers.
type IdentifierCase int

const (
	CaseLower IdentifierCase = iota
	CaseUpper
	CasePreserve
)

// Dialect is an immutable description of one SQL dialect.
type Dialect struct {
	Name       string
	Parent     string
	IdentCase  IdentifierCase
	IdentQuote []Quote
	StrQuote   []Quote
	// DollarQuoting enables PostgreSQL-style $tag$...$tag$ strings.
	DollarQuoting bool
	// Keywords holds upper-case keywords recognised by the dialect.
	Keywords map[string]struct{}
	// Constructs records constructs the grammar accepts (informational; the
	// grammar is permissive regardless).
	Constructs []string
}

// IsKeyword reports whether s is a keyword in the dialect.
func (d *Dialect) IsKeyword(s string) bool {
	_, ok := d.Keywords[strings.ToUpper(s)]
	return ok
}

// HasConstruct reports whether the dialect advertises a construct.
func (d *Dialect) HasConstruct(name string) bool {
	for _, c := range d.Constructs {
		if c == name {
			return true
		}
	}
	return false
}

var (
	registry = map[string]*Dialect{}
	names    []string
)

// Lookup returns the named dialect. The name is case-insensitive.
func Lookup(name string) (*Dialect, bool) {
	if name == "" {
		name = "ansi"
	}
	d, ok := registry[strings.ToLower(name)]
	return d, ok
}

// MustLookup returns the named dialect or panics. Callers use it only for
// built-in names.
func MustLookup(name string) *Dialect {
	d, ok := Lookup(name)
	if !ok {
		panic("dialect: unknown dialect " + name)
	}
	return d
}

// Names returns all registered dialect names, sorted.
func Names() []string { return append([]string(nil), names...) }

// Has reports whether a dialect is registered.
func Has(name string) bool { _, ok := registry[strings.ToLower(name)]; return ok }

type dialectSpec struct {
	parent     string
	identCase  IdentifierCase
	identQuote []Quote
	strQuote   []Quote
	dollar     bool
	extra      []string
	constructs []string
}

// baseKeywords is the ANSI keyword set. Dialects add to it.
var baseKeywords = splitKeywords(`
ADD ALL ALTER AND ANY AS ASC AUTHORIZATION BEGIN BETWEEN BOTH BY CASCADE CASE CAST
CHECK COLLATE COLUMN COMMIT CONNECT CONSTRAINT CREATE CROSS CURRENT CURRENT_DATE
CURRENT_TIME CURRENT_TIMESTAMP CURRENT_USER CURSOR DATABASE DECIMAL DECLARE DEFAULT
DELETE DESC DISTINCT DO DROP ELSE END ESCAPE EXCEPT EXEC EXECUTE EXISTS EXPLAIN
FALSE FETCH FIRST FLOAT FOR FOREIGN FROM FULL FUNCTION GRANT GROUP HAVING IF IN
INDEX INNER INSERT INT INTEGER INTERSECT INTO IS JOIN KEY LANGUAGE LAST LATERAL
LEADING LEFT LIKE LIMIT LOCAL NATURAL NOT NULL NULLS OFFSET ON ONLY OPEN OR ORDER
OUTER OVER PARTITION PRECISION PRIMARY PROCEDURE PUBLIC RECURSIVE REFERENCES
RETURNING REVOKE RIGHT ROLLBACK ROW ROWS SCHEMA SELECT SESSION SET SOME TABLE
TEMP TEMPORARY THEN TO TRANSACTION TRIGGER TRUE TRUNCATE UNION UNIQUE UNKNOWN
UPDATE USER USING VALUES VARCHAR VIEW WHEN WHERE WITH WORK WRITE
`)

func splitKeywords(s string) map[string]struct{} {
	m := map[string]struct{}{}
	for _, f := range strings.Fields(s) {
		m[strings.ToUpper(f)] = struct{}{}
	}
	return m
}

func register(name string, spec dialectSpec) {
	d := &Dialect{
		Name:          name,
		Parent:        spec.parent,
		IdentCase:     spec.identCase,
		IdentQuote:    spec.identQuote,
		StrQuote:      spec.strQuote,
		DollarQuoting: spec.dollar,
		Keywords:      map[string]struct{}{},
		Constructs:    append([]string(nil), spec.constructs...),
	}
	if spec.parent != "" {
		if p, ok := registry[spec.parent]; ok {
			for k := range p.Keywords {
				d.Keywords[k] = struct{}{}
			}
		}
	} else {
		for k := range baseKeywords {
			d.Keywords[k] = struct{}{}
		}
	}
	for _, k := range spec.extra {
		d.Keywords[strings.ToUpper(k)] = struct{}{}
	}
	registry[strings.ToLower(name)] = d
	names = append(names, strings.ToLower(name))
}

func init() {
	ansiQuotes := []Quote{{'"', '"'}}
	strQuote := []Quote{{'\'', '\''}}
	backtick := []Quote{{'`', '`'}}

	register("ansi", dialectSpec{
		identCase:  CaseLower,
		identQuote: ansiQuotes,
		strQuote:   strQuote,
	})

	// Close ANSI relatives.
	register("athena", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("EXTERNAL MSCK PARTITIONED REPAIR SERDEPROPERTIES TBLPROPERTIES"),
		constructs: []string{"array", "map", "struct", "unnest"},
	})
	register("bigquery", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: backtick,
		strQuote:   []Quote{{'\'', '\''}, {'"', '"'}},
		extra:      strings.Fields("ARRAY STRUCT UNNEST QUALIFY PIVOT UNPIVOT OPTIONS EXCEPT REPLACE"),
		constructs: []string{"array", "struct", "unnest", "qualify", "pivot", "match_recognize"},
	})
	register("clickhouse", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("FINAL SAMPLE PREWHERE TTL MATERIALIZE SETTINGS FORMAT"),
		constructs: []string{"array_join", "prewhere", "final"},
	})
	register("databricks", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("DELTA USING OPTIMIZE VACUUM ZORDER MERGE QUALIFY LATERAL"),
		constructs: []string{"qualify", "lateral", "merge", "delta"},
	})
	register("db2", dialectSpec{
		parent: "ansi", identCase: CaseUpper, identQuote: ansiQuotes, strQuote: strQuote,
		extra:      strings.Fields("FETCH ORGANIZE HIERARCHY OPTIMIZE"),
		constructs: []string{"fetch_first", "hierarchy"},
	})
	register("doris", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("DISTRIBUTED BUCKETS PROPERTIES DUPLICATE"),
		constructs: []string{"distributed", "buckets"},
	})
	register("duckdb", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: ansiQuotes, strQuote: strQuote,
		extra:      strings.Fields("PIVOT UNPIVOT QUALIFY SAMPLE ASOF POSITIONAL"),
		constructs: []string{"pivot", "unpivot", "qualify", "asof"},
	})
	register("exasol", dialectSpec{
		parent: "ansi", identCase: CaseUpper, identQuote: ansiQuotes, strQuote: strQuote,
		extra:      strings.Fields("CONNECT EMITS LOCAL PRELOADING"),
		constructs: []string{"connect_by"},
	})
	register("flink", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("TUMBLE HOP CUMULATE WATERMARK MATCH_RECOGNIZE"),
		constructs: []string{"window", "watermark", "match_recognize"},
	})
	register("greenplum", dialectSpec{
		parent: "postgres", identCase: CaseLower, identQuote: ansiQuotes, strQuote: strQuote, dollar: true,
		extra:      strings.Fields("DISTRIBUTED"),
		constructs: []string{"distributed_by"},
	})
	register("hive", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: backtick,
		strQuote:   []Quote{{'\'', '\''}, {'"', '"'}},
		extra:      strings.Fields("LATERAL VIEW PARTITIONED CLUSTERED SORTED SERDE STORED TBLPROPERTIES"),
		constructs: []string{"lateral_view", "partitioned"},
	})
	register("impala", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: backtick,
		strQuote:   []Quote{{'\'', '\''}, {'"', '"'}},
		extra:      strings.Fields("REFRESH INVALIDATE METADATA COMPUTE STATS"),
		constructs: []string{"compute_stats"},
	})
	register("mariadb", dialectSpec{
		parent: "mysql", identCase: CasePreserve, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("RETURNING"),
		constructs: []string{"returning"},
	})
	register("materialize", dialectSpec{
		parent: "postgres", identCase: CaseLower, identQuote: ansiQuotes, strQuote: strQuote, dollar: true,
		extra:      strings.Fields("CLUSTER SOURCES SINKS"),
		constructs: []string{"sources", "sinks", "clusters"},
	})
	register("mysql", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: backtick, strQuote: strQuote,
		extra: strings.Fields(`INTO OUTFILE DUMPFILE STRAIGHT_JOIN SQL_CALC_FOUND_ROWS
			LOCK UNLOCK SHOW DESCRIBE USE FORCE IGNORE LOW_PRIORITY HIGH_PRIORITY
			ZEROFILL UNSIGNED AUTO_INCREMENT ENGINE CHARSET COLLATE`),
		constructs: []string{"on_duplicate_key_update", "straight_join", "window"},
	})
	register("oracle", dialectSpec{
		parent: "ansi", identCase: CaseUpper, identQuote: ansiQuotes, strQuote: strQuote,
		extra: strings.Fields(`CONNECT PRIOR START WITH MINUS ROWNUM NVL NVL2 DECODE
			DUAL SYSDATE SYSTIMESTAMP FLASHBACK PIVOT UNPIVOT MODEL QUALIFY`),
		constructs: []string{"connect_by", "rownum", "minus", "pivot", "qualify", "fetch_first"},
	})
	register("postgres", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: ansiQuotes, strQuote: strQuote, dollar: true,
		extra: strings.Fields(`ILIKE SIMILAR RETURNING SERIAL BIGSERIAL UUID JSONB JSON
			LATERAL MATERIALIZED CONCURRENTLY VACUUM ANALYZE COPY GENERATED OVERLAPS
			ARRAY UNNEST DISTINCT ON FILTER WINDOW WITHIN GROUP`),
		constructs: []string{"returning", "lateral", "json", "merge", "generated", "filter", "window"},
	})
	register("redshift", dialectSpec{
		parent: "postgres", identCase: CaseLower, identQuote: ansiQuotes, strQuote: strQuote, dollar: true,
		extra:      strings.Fields("DISTKEY SORTKEY ENCODE COMPOUND INTERLEAVED SUPER"),
		constructs: []string{"distkey", "sortkey", "super"},
	})
	register("snowflake", dialectSpec{
		parent: "ansi", identCase: CaseUpper, identQuote: ansiQuotes, strQuote: strQuote, dollar: true,
		extra: strings.Fields(`QUALIFY SAMPLE FLATTEN VARIANT LATERAL PIVOT UNPIVOT
			MATCH_RECOGNIZE CLONE TASK STREAM TIME TRAVEL AT BEFORE`),
		constructs: []string{"qualify", "flatten", "variant", "pivot", "match_recognize", "time_travel"},
	})
	register("sparksql", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("LATERAL QUALIFY DISTRIBUTE CLUSTER SORTED BUCKETS TBLPROPERTIES"),
		constructs: []string{"lateral", "qualify", "distribute_by"},
	})
	register("sqlite", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: []Quote{{'"', '"'}, {'[', ']'}, {'`', '`'}},
		strQuote:   strQuote,
		extra:      strings.Fields("AUTOINCREMENT WITHOUT ROWID GLOB PRAGMA ATTACH DETACH VACUUM"),
		constructs: []string{"without_rowid", "returning", "window", "vacuum"},
	})
	register("starrocks", dialectSpec{
		parent: "mysql", identCase: CaseLower, identQuote: backtick, strQuote: strQuote,
		extra:      strings.Fields("DISTRIBUTED BUCKETS PROPERTIES"),
		constructs: []string{"distributed", "buckets"},
	})
	register("teradata", dialectSpec{
		parent: "ansi", identCase: CaseUpper, identQuote: ansiQuotes, strQuote: strQuote,
		extra:      strings.Fields("QUALIFY SAMPLE TOP VOLATILE MULTISET SET"),
		constructs: []string{"qualify", "top", "volatile"},
	})
	register("trino", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: ansiQuotes, strQuote: strQuote,
		extra:      strings.Fields("UNNEST LATERAL TABLESAMPLE MATCH_RECOGNIZE"),
		constructs: []string{"unnest", "lateral", "tablesample", "match_recognize"},
	})
	register("tsql", dialectSpec{
		parent: "ansi", identCase: CasePreserve, identQuote: []Quote{{'"', '"'}, {'[', ']'}},
		strQuote: strQuote,
		extra: strings.Fields(`TOP GO TRY CATCH THROW MERGE OUTPUT PIVOT UNPIVOT
			NOCOUNT XACT_ABORT IDENTITY CLUSTERED NONCLUSTERED NVARCHAR NCHAR DATETIME2
			DATETIMEOFFSET UNIQUEIDENTIFIER STRING_AGG GENERATE_SERIES`),
		constructs: []string{"top", "merge", "output", "try_catch", "pivot", "generate_series"},
	})
	register("vertica", dialectSpec{
		parent: "ansi", identCase: CaseLower, identQuote: ansiQuotes, strQuote: strQuote,
		extra:      strings.Fields("PROJECTION SEGMENTED UNSEGMENTED KSAFE"),
		constructs: []string{"projection"},
	})

	sort.Strings(names)
}

// All returns every dialect in name order.
func All() []*Dialect {
	out := make([]*Dialect, 0, len(names))
	for _, n := range names {
		out = append(out, registry[n])
	}
	return out
}
