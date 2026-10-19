package rules

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
)

type casing int

const (
	caseOther casing = iota
	caseUpper
	caseLower
	caseCapitalised
)

func classifyCase(s string) casing {
	if s == "" {
		return caseOther
	}
	hasUpper, hasLower := false, false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			hasUpper = true
		} else if r >= 'a' && r <= 'z' {
			hasLower = true
		}
	}
	switch {
	case hasUpper && !hasLower:
		return caseUpper
	case hasLower && !hasUpper:
		return caseLower
	case hasUpper && hasLower:
		// Capitalised: first letter upper, the rest lower.
		first := rune(s[0])
		if first >= 'A' && first <= 'Z' && strings.ToLower(s[1:]) == s[1:] {
			return caseCapitalised
		}
	}
	return caseOther
}

func applyCase(s string, c casing) string {
	switch c {
	case caseUpper:
		return strings.ToUpper(s)
	case caseLower:
		return strings.ToLower(s)
	case caseCapitalised:
		lower := strings.ToLower(s)
		if lower == "" {
			return lower
		}
		return strings.ToUpper(lower[:1]) + lower[1:]
	}
	return s
}

type capitalisation struct {
	meta      Meta
	policyKey string
	kind      string
}

func (r *capitalisation) Meta() Meta { return r.meta }

func (r *capitalisation) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	toks := ctx.Tokens
	type cand struct {
		idx int
		c   casing
	}
	var cands []cand
	for i := range toks {
		if !r.matches(toks, i) {
			continue
		}
		c := classifyCase(toks[i].Text)
		if c == caseOther {
			continue
		}
		cands = append(cands, cand{idx: i, c: c})
	}
	if len(cands) == 0 {
		return nil
	}

	policy := ctx.Settings.RuleString(r.meta.Name, r.policyKey, "consistent")
	target := caseOther
	if policy != "consistent" && policy != "" {
		switch strings.ToLower(policy) {
		case "upper", "upper_case", "uppercase":
			target = caseUpper
		case "lower", "lower_case", "lowercase":
			target = caseLower
		case "capitalise", "capitalize", "capitalised":
			target = caseCapitalised
		}
	} else {
		counts := map[casing]int{}
		for _, c := range cands {
			counts[c.c]++
		}
		best, bestN := caseOther, 0
		tie := false
		for _, c := range []casing{caseUpper, caseLower, caseCapitalised} {
			if counts[c] > bestN {
				best, bestN, tie = c, counts[c], false
			} else if counts[c] == bestN && counts[c] > 0 {
				tie = true
			}
		}
		if tie {
			return nil
		}
		target = best
	}
	if target == caseOther {
		return nil
	}

	var out []diagnostic.Diagnostic
	for _, c := range cands {
		if c.c == target {
			continue
		}
		tok := toks[c.idx]
		d := diag(r.meta, tok.Span, "Inconsistent capitalisation of "+r.kind+".")
		d.Fix = &diagnostic.Fix{Edits: []diagnostic.Edit{edit(tok.Span, applyCase(tok.Text, target))}}
		d.Fixability = r.meta.Fixable
		d.Help = "Use " + applyCase(tok.Text, target)
		out = append(out, d)
	}
	return out
}

func (r *capitalisation) matches(toks []lexer.Token, i int) bool {
	t := toks[i]
	switch r.kind {
	case "keywords":
		return t.Kind == lexer.Keyword && !isDatatype(t.Text) &&
			t.Upper() != "TRUE" && t.Upper() != "FALSE" && t.Upper() != "NULL"
	case "identifiers":
		if t.Kind != lexer.Identifier || isDatatype(t.Text) {
			return false
		}
		if n := nextSig(toks, i); n >= 0 && toks[n].Kind == lexer.LParen {
			return false // function name
		}
		return true
	case "functions":
		if t.Kind != lexer.Identifier && t.Kind != lexer.Keyword {
			return false
		}
		n := nextSig(toks, i)
		return n >= 0 && toks[n].Kind == lexer.LParen
	case "literals":
		if t.Kind != lexer.Keyword {
			return false
		}
		u := t.Upper()
		return u == "TRUE" || u == "FALSE" || u == "NULL"
	case "datatypes":
		return (t.Kind == lexer.Keyword || t.Kind == lexer.Identifier) && isDatatype(t.Text)
	}
	return false
}

func init() {
	Register(&capitalisation{meta: newMeta("CP01", "capitalisation.keywords", "capitalisation", Style, Token, diagnostic.Safe, "Inconsistent capitalisation of keywords.", "capitalisation-policy"), policyKey: "capitalisation-policy", kind: "keywords"})
	Register(&capitalisation{meta: newMeta("CP02", "capitalisation.identifiers", "capitalisation", Style, Token, diagnostic.Safe, "Inconsistent capitalisation of unquoted identifiers.", "extended-capitalisation-policy", "unquoted-identifiers-policy"), policyKey: "extended-capitalisation-policy", kind: "identifiers"})
	Register(&capitalisation{meta: newMeta("CP03", "capitalisation.functions", "capitalisation", Style, Token, diagnostic.Safe, "Inconsistent capitalisation of function names.", "extended-capitalisation-policy"), policyKey: "extended-capitalisation-policy", kind: "functions"})
	Register(&capitalisation{meta: newMeta("CP04", "capitalisation.literals", "capitalisation", Style, Token, diagnostic.Safe, "Inconsistent capitalisation of boolean/null literals.", "capitalisation-policy"), policyKey: "capitalisation-policy", kind: "literals"})
	Register(&capitalisation{meta: newMeta("CP05", "capitalisation.types", "capitalisation", Style, Token, diagnostic.Safe, "Inconsistent capitalisation of datatypes.", "extended-capitalisation-policy"), policyKey: "extended-capitalisation-policy", kind: "datatypes"})
}
