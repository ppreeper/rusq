package templater

import (
	"regexp"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
)

type placeholderTemplater struct{}

var placeholderStyles = map[string]struct {
	re    string
	value byte
}{
	"colon":           {`:[A-Za-z_][A-Za-z0-9_]*`, 'x'},
	"colon_nospaces":  {`:[A-Za-z_][A-Za-z0-9_]*`, 'x'},
	"numeric_colon":   {`:[0-9]+`, '0'},
	"pyformat":        {`%\([A-Za-z_][A-Za-z0-9_]*\)s`, 'x'},
	"dollar":          {`\$[A-Za-z_][A-Za-z0-9_]*`, 'x'},
	"dollar_surround": {`\$\{[A-Za-z_][A-Za-z0-9_]*\}`, 'x'},
	"question_mark":   {`\?`, 'x'},
	"numeric_dollar":  {`\$[0-9]+`, '0'},
	"percent":         {`%s`, 'x'},
	"ampersand":       {`&[A-Za-z_][A-Za-z0-9_]*`, 'x'},
	"apache_camel":    {`\{[A-Za-z_][A-Za-z0-9_]*\}`, 'x'},
	"at":              {`@[A-Za-z_][A-Za-z0-9_]*`, 'x'},
}

func (placeholderTemplater) Render(src source.Text, s settings.Settings) ([]Variant, []diagnostic.SyntaxError) {
	style := "colon"
	value := byte('x')
	var re *regexp.Regexp
	if cfg, ok := s.TemplaterConfig["placeholder"]; ok {
		if v, ok := cfg["param-style"].(string); ok && v != "" {
			style = v
		} else if v, ok := cfg["param_style"].(string); ok && v != "" {
			style = v
		}
		if v, ok := cfg["param-regex"].(string); ok && v != "" {
			if compiled, err := regexp.Compile(v); err == nil {
				re = compiled
			}
		}
	}
	if st, ok := placeholderStyles[style]; ok {
		if re == nil {
			re = regexp.MustCompile(st.re)
		}
		value = st.value
	} else if re == nil {
		re = regexp.MustCompile(placeholderStyles["colon"].re)
	}

	var reps []replacement
	for _, loc := range re.FindAllIndex(src.Bytes, -1) {
		if style == "colon" || style == "colon_nospaces" {
			// Do not treat a PostgreSQL cast (::type) as a parameter.
			if loc[0] > 0 && src.Bytes[loc[0]-1] == ':' {
				continue
			}
		}
		reps = append(reps, replacement{start: loc[0], end: loc[1], value: value})
	}
	if len(reps) == 0 {
		return []Variant{identity(src)}, nil
	}
	return []Variant{buildVariant(src, reps, []string{"placeholder:" + style})}, nil
}

type pythonTemplater struct{}

var pythonExpr = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*(?:![a-zA-Z])?(?::[^}]*)?\}`)

func (pythonTemplater) Render(src source.Text, s settings.Settings) ([]Variant, []diagnostic.SyntaxError) {
	var reps []replacement
	for _, loc := range pythonExpr.FindAllIndex(src.Bytes, -1) {
		reps = append(reps, replacement{start: loc[0], end: loc[1], value: 'x'})
	}
	if len(reps) == 0 {
		return []Variant{identity(src)}, nil
	}
	return []Variant{buildVariant(src, reps, []string{"python"})}, nil
}
