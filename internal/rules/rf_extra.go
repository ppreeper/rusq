package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/ast"
	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
)

// RF01 — references must come from a table in FROM.
type rf01 struct{ meta Meta }

func (r *rf01) Meta() Meta { return r.meta }
func (r *rf01) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || len(allTables(st.Select)) == 0 {
			continue
		}
		known := tableNames(st.Select)
		sigs := significantOf(st.Node.Leaves(nil))
		reported := map[string]bool{}
		for i := 0; i+2 < len(sigs); i++ {
			if !isNameToken(sigs[i].Kind) || sigs[i+1].Kind != lexer.Dot || !isNameToken(sigs[i+2].Kind) {
				continue
			}
			q := strings.ToLower(unquote(sigs[i].Text))
			if known[q] || reported[q] {
				continue
			}
			// A three-part name (schema.table.column) is treated as known.
			if i+3 < len(sigs) && sigs[i+3].Kind == lexer.Dot {
				continue
			}
			reported[q] = true
			out = append(out, diag(r.meta, sigs[i].Span, "Reference "+unquote(sigs[i].Text)+" is not present in FROM."))
		}
	}
	return out
}

// RF02 — qualify column references when more than one table is present.
type rf02 struct{ meta Meta }

func (r *rf02) Meta() Meta { return r.meta }
func (r *rf02) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || len(allTables(st.Select)) < 2 {
			continue
		}
		for _, col := range st.Select.Columns {
			if col.Star || len(col.Tokens) != 1 || !isNameToken(col.Tokens[0].Kind) {
				continue
			}
			out = append(out, diag(r.meta, col.Tokens[0].Span, "Column reference should be qualified when multiple tables are present."))
		}
	}
	return out
}

// RF03 — consistent qualification in single-table statements.
type rf03 struct{ meta Meta }

func (r *rf03) Meta() Meta { return r.meta }
func (r *rf03) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil || len(allTables(st.Select)) != 1 {
			continue
		}
		type ref struct {
			col  *Column
			qual bool
		}
		var refs []ref
		for _, col := range st.Select.Columns {
			if col.Star {
				continue
			}
			qual := len(col.Tokens) >= 3 && col.Tokens[1].Kind == lexer.Dot
			refs = append(refs, ref{col: col, qual: qual})
		}
		var nQual, nPlain int
		for _, rf := range refs {
			if rf.qual {
				nQual++
			} else {
				nPlain++
			}
		}
		if nQual == 0 || nPlain == 0 {
			continue
		}
		wantQual := nQual >= nPlain
		for _, rf := range refs {
			if rf.qual != wantQual && len(rf.col.Tokens) > 0 {
				msg := "Column references should be qualified consistently."
				if wantQual {
					msg = "Column references should be qualified."
				} else {
					msg = "Column references should not be qualified."
				}
				out = append(out, diag(r.meta, rf.col.Tokens[0].Span, msg))
			}
		}
	}
	return out
}

// RF04 — keywords should not be used as identifiers.
type rf04 struct{ meta Meta }

func (r *rf04) Meta() Meta { return r.meta }
func (r *rf04) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for _, t := range ctx.Tokens {
		if t.Kind != lexer.QuotedIdentifier {
			continue
		}
		inner := unquote(t.Text)
		if ctx.Dialect.IsKeyword(inner) {
			out = append(out, diag(r.meta, t.Span, "Keyword "+inner+" should not be used as an identifier."))
		}
	}
	return out
}

// RF05 — no special characters in identifiers.
type rf05 struct{ meta Meta }

func (r *rf05) Meta() Meta { return r.meta }
func (r *rf05) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	allowSpace := ctx.Settings.RuleBool(r.meta.Name, "allow-space-in-identifier", false)
	var out []diagnostic.Diagnostic
	for _, t := range ctx.Tokens {
		var name string
		switch t.Kind {
		case lexer.Identifier:
			name = t.Text
		case lexer.QuotedIdentifier:
			name = unquote(t.Text)
		default:
			continue
		}
		if hasSpecialChars(name, allowSpace) {
			out = append(out, diag(r.meta, t.Span, "Identifier contains special characters."))
		}
	}
	return out
}

func hasSpecialChars(s string, allowSpace bool) bool {
	for _, r := range s {
		if r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		if allowSpace && r == ' ' {
			continue
		}
		return true
	}
	return false
}

// RF07 — do not reference a column alias in its own OVER clause.
type rf07 struct{ meta Meta }

func (r *rf07) Meta() Meta { return r.meta }
func (r *rf07) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.AST == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, st := range ctx.AST.Statements {
		if st.Select == nil {
			continue
		}
		for _, col := range st.Select.Columns {
			if col.Alias == "" {
				continue
			}
			overIdx := -1
			for i, t := range col.Tokens {
				if t.Kind == lexer.Keyword && t.Upper() == "OVER" {
					overIdx = i
					break
				}
			}
			if overIdx < 0 {
				continue
			}
			for i := overIdx + 1; i < len(col.Tokens); i++ {
				if isNameToken(col.Tokens[i].Kind) && strings.EqualFold(unquote(col.Tokens[i].Text), col.Alias) {
					out = append(out, diag(r.meta, col.Tokens[i].Span, "Column alias should not be referenced inside its own OVER clause."))
					break
				}
			}
		}
	}
	return out
}

// Column is re-exported locally for readability.
type Column = ast.Column

func init() {
	Register(&rf01{meta: newMeta("RF01", "references.from", "references", Correctness, Tree, diagnostic.NoFix, "References cannot reference objects not present in FROM.", "force-enable")})
	Register(&rf02{meta: newMeta("RF02", "references.qualification", "references", Suspicious, Tree, diagnostic.NoFix, "References should be qualified if more than one table is referenced.", "subqueries-ignore-external-references")})
	Register(&rf03{meta: newMeta("RF03", "references.consistent", "references", Style, Tree, diagnostic.NoFix, "Column references should be qualified consistently in single-table statements.", "single-table-references", "force-enable")})
	Register(&rf04{meta: newMeta("RF04", "references.keywords", "references", Style, Token, diagnostic.NoFix, "Keywords should not be used as identifiers.", "ignore-words", "ignore-words-regex")})
	Register(&rf05{meta: newMeta("RF05", "references.special_chars", "references", Style, Token, diagnostic.NoFix, "Do not use special characters in identifiers.", "allow-space-in-identifier", "ignore-words", "ignore-words-regex")})
	Register(&rf07{meta: newMeta("RF07", "references.window_alias", "references", Correctness, Tree, diagnostic.NoFix, "Do not reference a column alias inside its own OVER clause.", "force-enable")})
}
