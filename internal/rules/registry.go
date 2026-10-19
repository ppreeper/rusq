// Package rules holds the rule registry and the rule implementations.
package rules

import (
	"sort"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

// Category groups rules and decides the default set.
type Category string

const (
	Correctness Category = "correctness"
	Suspicious  Category = "suspicious"
	Style       Category = "style"
	Security    Category = "security"
	Pedantic    Category = "pedantic"
	Restriction Category = "restriction"
	Formatting  Category = "formatting"
)

// Default reports whether the category is on in the default set.
func (c Category) Default() bool {
	switch c {
	case Correctness, Suspicious, Security:
		return true
	}
	return false
}

// Severity maps a category to its default severity.
func (c Category) Severity() diagnostic.Severity {
	switch c {
	case Correctness:
		return diagnostic.Error
	case Formatting, Pedantic:
		return diagnostic.Hint
	}
	return diagnostic.Warning
}

// Source is the cheapest pass a rule needs.
type Source int

const (
	Token Source = iota
	Lines
	Tree
	File
	Index
)

func (s Source) String() string {
	switch s {
	case Token:
		return "token"
	case Lines:
		return "lines"
	case Tree:
		return "tree"
	case File:
		return "file"
	case Index:
		return "index"
	}
	return "unknown"
}

// Meta describes a rule.
type Meta struct {
	Code        string
	Name        string
	Family      string
	Category    Category
	Severity    diagnostic.Severity
	Source      Source
	Fixable     diagnostic.Fixability
	Default     bool
	Stable      bool
	Dialects    []string
	ConfigKeys  []string
	Description string
}

// Rule is one lint rule.
type Rule interface {
	Meta() Meta
	Check(*lint.Context) []diagnostic.Diagnostic
}

var registry []Rule

// Register adds a rule to the registry. It is called from each family's init.
func Register(r Rule) { registry = append(registry, r) }

// All returns the registry sorted by code.
func All() []Rule {
	out := append([]Rule(nil), registry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Meta().Code < out[j].Meta().Code })
	return out
}

// ByCode returns the rule with the given code.
func ByCode(code string) (Rule, bool) {
	for _, r := range registry {
		if r.Meta().Code == code {
			return r, true
		}
	}
	return nil, false
}

// Codes returns every registered code.
func Codes() []string {
	out := make([]string, 0, len(registry))
	for _, r := range registry {
		out = append(out, r.Meta().Code)
	}
	sort.Strings(out)
	return out
}

// WithSource returns the rules that run at or before the given source tier.
func WithSource(s Source) []Rule {
	var out []Rule
	for _, r := range All() {
		if r.Meta().Source <= s {
			out = append(out, r)
		}
	}
	return out
}

// Helper constructors used by the family files.

func newMeta(code, name, family string, cat Category, src Source, fix diagnostic.Fixability, desc string, keys ...string) Meta {
	return Meta{
		Code:        code,
		Name:        name,
		Family:      family,
		Category:    cat,
		Severity:    cat.Severity(),
		Source:      src,
		Fixable:     fix,
		Default:     cat.Default(),
		Stable:      true,
		ConfigKeys:  keys,
		Description: desc,
	}
}

func diag(m Meta, span source.Range, msg string) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{
		Code:     m.Code,
		Name:     m.Name,
		Severity: m.Severity,
		Span:     span,
		Message:  msg,
	}
}

func withFix(d diagnostic.Diagnostic, repl string, edits ...diagnostic.Edit) diagnostic.Diagnostic {
	d.Fix = &diagnostic.Fix{Edits: edits}
	if repl != "" {
		d.Help = repl
	}
	return d
}

func edit(span source.Range, replacement string) diagnostic.Edit {
	return diagnostic.Edit{Span: span, Replacement: replacement}
}
