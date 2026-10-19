// Package selector implements rusq's rule selector grammar:
// ALL < category < group < prefix < code.
package selector

import "strings"

// Set is a resolved include/exclude pair.
type Set struct {
	Include []string
	Exclude []string
}

// Matches reports whether one selector matches a rule.
func Matches(sel, code, name, family, category string) bool {
	s := strings.TrimSpace(sel)
	if s == "" {
		return false
	}
	if strings.EqualFold(s, "ALL") {
		return true
	}
	if strings.EqualFold(s, "core") {
		return false // handled by the caller via the default set
	}
	if strings.EqualFold(s, code) || strings.EqualFold(s, name) {
		return true
	}
	if strings.EqualFold(s, family) || strings.EqualFold(s, category) {
		return true
	}
	// A prefix such as LT or a legacy alias such as L003.
	if len(s) >= 2 && isUpperPrefix(s) && strings.HasPrefix(strings.ToUpper(code), strings.ToUpper(s)) {
		return true
	}
	return false
}

// HasAll reports whether the set includes the ALL selector.
func (s Set) HasAll() bool {
	for _, inc := range s.Include {
		if strings.EqualFold(strings.TrimSpace(inc), "ALL") {
			return true
		}
	}
	return false
}

// HasCore reports whether the set includes the core selector.
func (s Set) HasCore() bool {
	for _, inc := range s.Include {
		if strings.EqualFold(strings.TrimSpace(inc), "core") {
			return true
		}
	}
	return false
}

// Selects resolves whether a rule is enabled.
func (s Set) Selects(code, name, family, category string, def bool) bool {
	enabled := def
	if len(s.Include) > 0 {
		enabled = false
		for _, inc := range s.Include {
			if strings.EqualFold(strings.TrimSpace(inc), "core") {
				if def {
					enabled = true
				}
				continue
			}
			if Matches(inc, code, name, family, category) {
				enabled = true
			}
		}
	}
	for _, exc := range s.Exclude {
		if Matches(exc, code, name, family, category) {
			enabled = false
		}
	}
	return enabled
}

// ExplicitCode reports whether sel names a specific rule code.
func ExplicitCode(sel string) bool {
	s := strings.TrimSpace(sel)
	if len(s) < 3 {
		return false
	}
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func isUpperPrefix(s string) bool {
	for _, c := range s {
		if !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}
