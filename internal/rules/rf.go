package rules

import (
	"regexp"
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/suppress"
)

// RF06 — unnecessary quoted identifier.
type rf06 struct{ meta Meta }

func (r *rf06) Meta() Meta { return r.meta }
func (r *rf06) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for _, t := range ctx.Tokens {
		if t.Kind != lexer.QuotedIdentifier {
			continue
		}
		inner := unquote(t.Text)
		if inner == t.Text || inner == "" {
			continue
		}
		if !plainIdentifier(inner) {
			continue
		}
		if ctx.Dialect.IsKeyword(inner) {
			continue
		}
		if strings.ToLower(inner) != inner && ctx.Dialect.IdentCase != dialect.CasePreserve {
			continue // unquoting would change case folding
		}
		d := diag(r.meta, t.Span, "Identifier does not require quoting.")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(t.Span, inner)}}
		d.Fixability = r.meta.Fixable
		out = append(out, d)
	}
	return out
}

// RUS101/RUS103 — validate suppression directives.
type rusDirectives struct{ meta Meta }

func (r *rusDirectives) Meta() Meta { return r.meta }
func (r *rusDirectives) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for _, d := range suppress.Parse(ctx.Tokens) {
		for _, code := range d.Codes {
			if legacyCode(code) {
				if r.meta.Code == "RUS103" {
					out = append(out, diag(r.meta, d.Span, "Suppression references deprecated code "+code+"."))
				}
				continue
			}
			if r.meta.Code == "RUS101" && !knownSelector(code) {
				out = append(out, diag(r.meta, d.Span, "Suppression references unknown rule "+code+"."))
			}
		}
	}
	return out
}

// RUS104 — surface recoverable parse errors.
type rus104 struct{ meta Meta }

func (r *rus104) Meta() Meta { return r.meta }
func (r *rus104) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.Tree == nil {
		return nil
	}
	var out []diagnostic.Diagnostic
	for _, e := range ctx.Tree.Errors {
		d := diag(r.meta, e.Span, e.Message)
		out = append(out, d)
	}
	return out
}

var legacyRe = regexp.MustCompile(`^L[0-9]{3}$`)

func legacyCode(s string) bool { return legacyRe.MatchString(strings.ToUpper(s)) }

func plainIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 0 {
			if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
				return false
			}
			continue
		}
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

func knownSelector(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	if u == "ALL" || strings.EqualFold(u, "core") {
		return true
	}
	for _, r := range All() {
		m := r.Meta()
		if strings.EqualFold(s, m.Code) || strings.EqualFold(s, m.Name) ||
			strings.EqualFold(s, m.Family) || strings.EqualFold(s, string(m.Category)) {
			return true
		}
		if len(u) == 2 && strings.HasPrefix(m.Code, u) {
			return true
		}
	}
	return false
}

func init() {
	Register(&rf06{meta: newMeta("RF06", "references.quoting", "references", Style, Token, diagnostic.Safe, "Unnecessary quoted identifier.", "prefer-quoted-identifiers", "ignore-words", "ignore-words-regex", "case-sensitive")})
	Register(&rusDirectives{meta: newMeta("RUS101", "rusq.invalid", "rusq", Correctness, Token, diagnostic.NoFix, "Suppression directive names an unknown rule.")})
	Register(&rusDirectives{meta: newMeta("RUS103", "rusq.deprecated", "rusq", Style, Token, diagnostic.NoFix, "Suppression directive references a deprecated code.")})
	Register(&rus104{meta: newMeta("RUS104", "rusq.parse_error", "rusq", Correctness, File, diagnostic.NoFix, "A region could not be parsed.")})
}
