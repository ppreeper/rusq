package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/tree"
)

// CV01 — consistent use of != or <>.
type cv01 struct{ meta Meta }

func (r *cv01) Meta() Meta { return r.meta }
func (r *cv01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	policy := ctx.Settings.RuleString(r.meta.Name, "preferred-not-equal-style", "consistent")
	want := ""
	switch strings.ToLower(policy) {
	case "ansi":
		want = "<>"
	case "c_style", "c-style":
		want = "!="
	}
	if want == "" {
		nAnsi, nC := 0, 0
		for _, t := range ctx.Tokens {
			if t.Kind == lexer.Operator {
				if t.Text == "<>" {
					nAnsi++
				} else if t.Text == "!=" {
					nC++
				}
			}
		}
		if nAnsi == nC {
			return nil
		}
		if nAnsi > nC {
			want = "<>"
		} else {
			want = "!="
		}
	}
	var out []diagnostic.Diagnostic
	for _, t := range ctx.Tokens {
		if t.Kind != lexer.Operator || (t.Text != "!=" && t.Text != "<>") || t.Text == want {
			continue
		}
		d := diag(r.meta, t.Span, "Use "+want+" instead of "+t.Text+".")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(t.Span, want)}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// CV02 — prefer COALESCE over IFNULL/NVL.
type cv02 struct{ meta Meta }

func (r *cv02) Meta() Meta { return r.meta }
func (r *cv02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Identifier && t.Kind != lexer.Keyword {
			continue
		}
		u := t.Upper()
		if u != "IFNULL" && u != "NVL" {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n < 0 || ctx.Tokens[n].Kind != lexer.LParen {
			continue
		}
		d := diag(r.meta, t.Span, "Use COALESCE instead of "+t.Text+".")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(t.Span, "COALESCE")}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// CV03 — trailing commas in the select clause.
type cv03 struct{ meta Meta }

func (r *cv03) Meta() Meta { return r.meta }
func (r *cv03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	mode := ctx.Settings.RuleString(r.meta.Name, "select-clause-trailing-comma", "forbid")
	var out []diagnostic.Diagnostic
	if ctx.AST == nil {
		return nil
	}
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, c := range st.Select.Clauses {
			if c.Kind != tree.SelectClause || len(c.Tokens) == 0 {
				continue
			}
			last := c.Tokens[len(c.Tokens)-1]
			hasComma := last.Kind == lexer.Comma
			switch strings.ToLower(mode) {
			case "require":
				if !hasComma && len(c.Tokens) > 2 {
					d := diag(r.meta, last.Span, "Select clause should end with a trailing comma.")
					d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(last.Span.End), ",")}}
					d.Fixability = r.meta.Fixable
					out = append(out, d)
				}
			default: // forbid
				if hasComma {
					d := diag(r.meta, last.Span, "Trailing comma in select clause.")
					d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(last.Span, "")}}
					d.Fixability = r.meta.Fixable
					out = append(out, d)
				}
			}
		}
	}
	return out
}

// CV05 — comparisons with NULL must use IS / IS NOT.
type cv05 struct{ meta Meta }

func (r *cv05) Meta() Meta { return r.meta }
func (r *cv05) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind != lexer.Operator || (t.Text != "=" && t.Text != "!=" && t.Text != "<>") {
			continue
		}
		n := nextSig(ctx.Tokens, i)
		if n < 0 || ctx.Tokens[n].Kind != lexer.Keyword || ctx.Tokens[n].Upper() != "NULL" {
			continue
		}
		repl := "IS"
		if t.Text != "=" {
			repl = "IS NOT"
		}
		d := diag(r.meta, t.Span, "Use "+repl+" when comparing with NULL.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(t.Span, repl)}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// CV06 — statements end with a semicolon.
type cv06 struct{ meta Meta }

func (r *cv06) Meta() Meta { return r.meta }
func (r *cv06) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if !ctx.Settings.RuleBool(r.meta.Name, "require-final-semicolon", true) {
		return nil
	}
	toks := ctx.Significant()
	if len(toks) == 0 {
		return nil
	}
	last := toks[len(toks)-1]
	if last.Kind == lexer.Semicolon {
		return nil
	}
	d := diag(r.meta, last.Span, "Statement must end with a semicolon.")
	d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(source.NewRangeAt(last.Span.End), ";")}}
	d.Fixability = r.meta.Fixable
	return []diagnostic.Diagnostic{d}
}

// CV08 — prefer LEFT JOIN over RIGHT JOIN.
type cv08 struct{ meta Meta }

func (r *cv08) Meta() Meta { return r.meta }
func (r *cv08) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for i, t := range ctx.Tokens {
		if t.Kind == lexer.Keyword && t.Upper() == "RIGHT" {
			n := nextSig(ctx.Tokens, i)
			if n >= 0 && ctx.Tokens[n].Kind == lexer.Keyword && ctx.Tokens[n].Upper() == "JOIN" {
				out = append(out, diag(r.meta, t.Span, "Use LEFT JOIN instead of RIGHT JOIN."))
			}
		}
	}
	return out
}

func init() {
	Register(&cv01{meta: newMeta("CV01", "convention.not_equal", "convention", Style, Token, diagnostic.Safe, "Consistent usage of != or <>.", "preferred-not-equal-style")})
	Register(&cv02{meta: newMeta("CV02", "convention.coalesce", "convention", Style, Token, diagnostic.Safe, "Use COALESCE instead of IFNULL/NVL.")})
	Register(&cv03{meta: newMeta("CV03", "convention.select_trailing_comma", "convention", Style, Tree, diagnostic.Safe, "Trailing commas within select clause.", "select-clause-trailing-comma")})
	Register(&cv05{meta: newMeta("CV05", "convention.is_null", "convention", Correctness, Token, diagnostic.Safe, "Comparisons with NULL should use IS / IS NOT.")})
	Register(&cv06{meta: newMeta("CV06", "convention.terminator", "convention", Style, File, diagnostic.Safe, "Statements must end with a semi-colon.", "require-final-semicolon", "multiline-newline")})
	Register(&cv08{meta: newMeta("CV08", "convention.left_join", "convention", Style, Token, diagnostic.NoFix, "Use LEFT JOIN instead of RIGHT JOIN.")})
}
