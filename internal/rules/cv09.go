package rules

import (
	"regexp"
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/lint"
)

// CV09 — block a configurable list of words.
type cv09 struct{ meta Meta }

func (r *cv09) Meta() Meta { return r.meta }
func (r *cv09) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	words := ruleStringList(ctx, r.meta.Name, "blocked-words")
	pattern := ctx.Settings.RuleString(r.meta.Name, "blocked-regex", "")
	matchSource := ctx.Settings.RuleBool(r.meta.Name, "match-source", true)
	if len(words) == 0 && pattern == "" {
		return nil
	}
	var re *regexp.Regexp
	if pattern != "" {
		re, _ = regexp.Compile("(?i)" + pattern)
	}
	blocked := func(s string) bool {
		low := strings.ToLower(s)
		for _, w := range words {
			if w != "" && strings.Contains(low, strings.ToLower(w)) {
				return true
			}
		}
		return re != nil && re.MatchString(s)
	}

	var out []diagnostic.Diagnostic
	seen := map[int]bool{}
	for _, t := range ctx.Tokens {
		if matchSource {
			if t.IsTrivia() || t.Kind == lexer.EOF || seen[t.Span.Start] {
				continue
			}
			if blocked(t.Text) {
				seen[t.Span.Start] = true
				out = append(out, diag(r.meta, t.Span, "Blocked word detected."))
			}
			continue
		}
		if !isNameToken(t.Kind) {
			continue
		}
		if blocked(unquote(t.Text)) {
			out = append(out, diag(r.meta, t.Span, "Blocked word "+unquote(t.Text)+" detected."))
		}
	}
	return out
}

func ruleStringList(ctx *lint.Context, rule, key string) []string {
	v, ok := ctx.Settings.RuleOption(rule, key)
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	}
	return nil
}

func init() {
	Register(&cv09{meta: newMeta("CV09", "convention.blocked_words", "convention", Restriction, Token, diagnostic.NoFix, "Block a configurable list of words.", "blocked-words", "blocked-regex", "match-source")})
}
