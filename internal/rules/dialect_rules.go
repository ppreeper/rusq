package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
)

// PG01 — avoid excessive locks.
type pg01 struct{ meta Meta }

func (r *pg01) Meta() Meta { return r.meta }
func (r *pg01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Upper() != "LOCK" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n < 0 || ctx.Tokens[n].Upper() != "TABLE" {
			continue
		}
		for j := n; j < len(ctx.Tokens); j++ {
			if ctx.Tokens[j].Upper() == "EXCLUSIVE" {
				out = append(out, diag(r.meta, t.Span, "Avoid excessive locks; ACCESS EXCLUSIVE blocks all access."))
				break
			}
			if ctx.Tokens[j].Kind == lexer.Semicolon {
				break
			}
		}
	}
	return out
}

// PG02 — create foreign keys as NOT VALID first.
type pg02 struct{ meta Meta }

func (r *pg02) Meta() Meta { return r.meta }
func (r *pg02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || t.Upper() != "FOREIGN" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n < 0 || ctx.Tokens[n].Upper() != "KEY" {
			continue
		}
		if !inAlterTable(ctx.Tokens, i) {
			continue
		}
		hasNotValid := false
		for j := n; j < len(ctx.Tokens); j++ {
			if ctx.Tokens[j].Kind == lexer.Semicolon {
				break
			}
			if ctx.Tokens[j].Kind == lexer.Keyword && ctx.Tokens[j].Upper() == "NOT" {
				k := nextSig(ctx.Tokens, j)
				if k >= 0 && ctx.Tokens[k].Upper() == "VALID" {
					hasNotValid = true
				}
			}
		}
		if !hasNotValid {
			out = append(out, diag(r.meta, t.Span, "Create foreign keys as NOT VALID, then validate them separately."))
		}
	}
	return out
}

func inAlterTable(tokens []lexer.Token, idx int) bool {
	seenAlter, seenTable := false, false
	for i := idx - 1; i >= 0 && i >= idx-12; i-- {
		if tokens[i].Kind == lexer.Semicolon {
			break
		}
		if tokens[i].Kind != lexer.Keyword {
			continue
		}
		switch tokens[i].Upper() {
		case "TABLE":
			seenTable = true
		case "ALTER":
			seenAlter = true
		}
	}
	return seenAlter && seenTable
}

// OR01 — remove empty batches (SQL*Plus "/" separators).
type or01 struct{ meta Meta }

func (r *or01) Meta() Meta { return r.meta }
func (r *or01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Operator || t.Text != "/" {
			continue
		}
		prev := prevSig(ctx.Tokens, i)
		next := nextSig(ctx.Tokens, i)
		alone := (prev < 0 || ctx.Tokens[prev].Line != t.Line) && (next < 0 || ctx.Tokens[next].Line != t.Line)
		if alone {
			out = append(out, diag(r.meta, t.Span, "Remove empty batch."))
		}
	}
	return out
}

// TQ01 — do not use the sp_ prefix for user procedures.
type tq01 struct{ meta Meta }

func (r *tq01) Meta() Meta { return r.meta }
func (r *tq01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for _, t := range ctx.Tokens {
		if !isNameToken(t.Kind) {
			continue
		}
		if strings.HasPrefix(strings.ToLower(unquote(t.Text)), "sp_") {
			out = append(out, diag(r.meta, t.Span, "The sp_ prefix is reserved for system procedures."))
		}
	}
	return out
}

// TQ02 — wrap multi-statement procedure bodies in BEGIN/END.
type tq02 struct{ meta Meta }

func (r *tq02) Meta() Meta { return r.meta }
func (r *tq02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Keyword || (t.Upper() != "PROCEDURE" && t.Upper() != "PROC") {
			continue
		}
		prev := prevSig(ctx.Tokens, i)
		if prev < 0 || ctx.Tokens[prev].Upper() != "CREATE" {
			continue
		}
		asIdx := -1
		for j := i; j < len(ctx.Tokens); j++ {
			if ctx.Tokens[j].Kind == lexer.Keyword && ctx.Tokens[j].Upper() == "AS" {
				asIdx = j
				break
			}
		}
		if asIdx < 0 {
			continue
		}
		stmts, hasBegin := 0, false
		for j := asIdx; j < len(ctx.Tokens); j++ {
			switch {
			case ctx.Tokens[j].Kind == lexer.Keyword && ctx.Tokens[j].Upper() == "BEGIN":
				hasBegin = true
			case ctx.Tokens[j].Kind == lexer.Semicolon:
				stmts++
			}
		}
		if stmts > 1 && !hasBegin {
			out = append(out, diag(r.meta, t.Span, "Procedure body with multiple statements should be wrapped in BEGIN/END."))
		}
	}
	return out
}

// TQ03 — remove empty batches.
type tq03 struct{ meta Meta }

func (r *tq03) Meta() Meta { return r.meta }
func (r *tq03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	sigs := ctx.Significant()
	for i, t := range sigs {
		if t.Kind != lexer.Keyword || t.Upper() != "GO" {
			continue
		}
		if n := nextSig(sigs, i); n >= 0 && sigs[n].Kind == lexer.Keyword && sigs[n].Upper() == "GO" {
			d := diag(r.meta, sigs[n].Span, "Empty batch detected.")
			d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(sigs[n].Span, "")}}
			d.Fixability = r.meta.Fixable
			out = append(out, d)
		}
	}
	return out
}

// TQ04 — prefer ANSI AS aliasing over alias = expression.
type tq04 struct{ meta Meta }

func (r *tq04) Meta() Meta { return r.meta }
func (r *tq04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, col := range st.Select.Columns {
			if len(col.Tokens) >= 3 && isNameToken(col.Tokens[0].Kind) &&
				col.Tokens[1].Kind == lexer.Operator && col.Tokens[1].Text == "=" {
				out = append(out, diag(r.meta, col.Tokens[0].Span, "Prefer ANSI-style AS aliasing over alias = expression."))
			}
		}
	}
	return out
}

// RUS102 is emitted by the linter's suppression pass; registering it makes it
// discoverable and selectable.
type rus102 struct{ meta Meta }

func (r *rus102) Meta() Meta                                      { return r.meta }
func (r *rus102) Check(ctx *lint.Context) []diagnostic.Diagnostic { return nil }

func init() {
	pgDialects := []string{"postgres", "greenplum", "materialize", "redshift"}
	m := newMeta("PG01", "postgres.excessive_locks", "postgres", Security, Token, diagnostic.NoFix, "Avoid excessive locks in PostgreSQL DDL.", "force-enable")
	m.Dialects = pgDialects
	Register(&pg01{meta: m})

	m = newMeta("PG02", "postgres.not_valid_foreign_key", "postgres", Security, Token, diagnostic.NoFix, "Create PostgreSQL foreign keys as NOT VALID before validating.", "force-enable")
	m.Dialects = pgDialects
	Register(&pg02{meta: m})

	m = newMeta("OR01", "oracle.empty_batch", "oracle", Style, Token, diagnostic.NoFix, "Remove empty batches.")
	m.Dialects = []string{"oracle"}
	Register(&or01{meta: m})

	m = newMeta("TQ01", "tsql.sp_prefix", "tsql", Style, Token, diagnostic.NoFix, "The sp_ prefix should not be used for user-defined procedures.")
	m.Dialects = []string{"tsql"}
	Register(&tq01{meta: m})

	m = newMeta("TQ02", "tsql.procedure_begin_end", "tsql", Style, Token, diagnostic.NoFix, "Procedure bodies with multiple statements should be wrapped in BEGIN/END.")
	m.Dialects = []string{"tsql"}
	Register(&tq02{meta: m})

	m = newMeta("TQ03", "tsql.empty_batch", "tsql", Style, Token, diagnostic.Safe, "Remove empty batches.")
	m.Dialects = []string{"tsql"}
	Register(&tq03{meta: m})

	m = newMeta("TQ04", "tsql.prefer_as_alias", "tsql", Style, Token, diagnostic.NoFix, "Prefer AS aliasing over alias = expression.")
	m.Dialects = []string{"tsql"}
	Register(&tq04{meta: m})

	Register(&rus102{meta: newMeta("RUS102", "rusq.unused", "rusq", Style, Token, diagnostic.NoFix, "Suppression directive silences nothing.")})
}
