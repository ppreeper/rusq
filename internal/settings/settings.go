// Package settings resolves effective, per-file settings from configuration
// and CLI overrides.
package settings

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ppreeper/rusq/internal/config"
	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/selector"
)

// FormatOptions controls the formatter.
type FormatOptions struct {
	IndentUnit          string
	IndentWidth         int
	LineLength          int
	LinePosition        string
	KeywordLinePosition string
	CommaStyle          string
	AlignFollowing      bool
	SingleTargetPolicy  string
	NewlineAtEOF        bool
	MaxBlankLines       int
	CteBracketNewline   bool
	// Types holds per-segment-type layout overrides keyed by segment name
	// ("comma", "operator", "keyword").
	Types map[string]TypeFormat
}

// TypeFormat is the layout override for one segment type.
type TypeFormat struct {
	LinePosition string
}

// TypeLinePosition returns the per-type line position for name, falling back to
// the supplied value when no override is configured.
func (f FormatOptions) TypeLinePosition(name, fallback string) string {
	if t, ok := f.Types[name]; ok && t.LinePosition != "" {
		return t.LinePosition
	}
	return fallback
}

// Settings is the resolved configuration for a run.
type Settings struct {
	DialectName        string
	Dialect            *dialect.Dialect
	Templater          string
	LineLength         int
	TargetVersion      string
	FileExts           []string
	RenderVariantLimit int
	MaxParseDepth      int
	MaxParseNodes      int
	Include            []string
	Exclude            []string
	Set                selector.Set
	Warnings           []string
	Fixable            []string
	Unfixable          []string
	PerFileIgnores     map[string][]string
	Preview            bool
	Index              bool
	IgnoreSuppressions bool
	DisableNoqa        bool
	Format             FormatOptions
	Rules              map[string]map[string]any
	TemplaterConfig    map[string]map[string]any
	ConfigFiles        []string
	ConfigDir          string
}

// Default returns the built-in defaults.
func Default() Settings {
	return Settings{
		DialectName:        "ansi",
		Dialect:            dialect.MustLookup("ansi"),
		Templater:          "raw",
		LineLength:         88,
		FileExts:           []string{".sql", ".dml", ".ddl"},
		RenderVariantLimit: 5,
		MaxParseDepth:      255,
		MaxParseNodes:      100000,
		Format: FormatOptions{
			IndentUnit:          "space",
			IndentWidth:         4,
			LineLength:          88,
			LinePosition:        "trailing",
			KeywordLinePosition: "leading",
			CommaStyle:          "trailing",
			SingleTargetPolicy:  "same_line",
			NewlineAtEOF:        true,
			MaxBlankLines:       1,
		},
	}
}

// FromConfig resolves settings from a merged config.
func FromConfig(c *config.Config, configDir string, files []string) (Settings, error) {
	s := Default()
	s.ConfigDir = configDir
	s.ConfigFiles = files
	if c == nil {
		return s, nil
	}
	if c.Dialect != "" {
		d, ok := dialect.Lookup(c.Dialect)
		if !ok {
			return s, fmt.Errorf("unknown dialect %q", c.Dialect)
		}
		s.DialectName = d.Name
		s.Dialect = d
	}
	if c.Templater != "" {
		s.Templater = c.Templater
	}
	if c.LineLength > 0 {
		s.LineLength = c.LineLength
		s.Format.LineLength = c.LineLength
	}
	if c.TargetVersion != "" {
		s.TargetVersion = c.TargetVersion
	}
	if len(c.SQLFileExts) > 0 {
		s.FileExts = c.SQLFileExts
	}
	if c.RenderVariantLimit > 0 {
		s.RenderVariantLimit = c.RenderVariantLimit
	}
	if c.MaxParseDepth > 0 {
		s.MaxParseDepth = c.MaxParseDepth
	}
	if c.MaxParseNodes > 0 {
		s.MaxParseNodes = c.MaxParseNodes
	}
	s.Include = c.Include
	s.Exclude = c.Exclude

	s.Set.Include = append(append([]string{}, c.Lint.Select...), c.Lint.ExtendSelect...)
	s.Set.Exclude = append(append([]string{}, c.Lint.Ignore...), c.Lint.ExtendIgnore...)
	s.Set.Exclude = append(s.Set.Exclude, c.Lint.ExcludeRules...)
	s.Warnings = c.Lint.Warnings
	s.Fixable = c.Lint.Fixable
	s.Unfixable = c.Lint.Unfixable
	s.PerFileIgnores = c.Lint.PerFileIgnores
	s.Preview = c.Lint.Preview
	s.Index = c.Index.Enabled
	s.Rules = c.Rules
	s.TemplaterConfig = c.TemplaterCfg

	if c.Format.IndentUnit != "" {
		s.Format.IndentUnit = c.Format.IndentUnit
	}
	if c.Format.IndentWidth > 0 {
		s.Format.IndentWidth = c.Format.IndentWidth
	}
	if c.Format.LinePosition != "" {
		s.Format.LinePosition = c.Format.LinePosition
	}
	if c.Format.KeywordLinePosition != "" {
		s.Format.KeywordLinePosition = c.Format.KeywordLinePosition
	}
	if c.Format.CommaStyle != "" {
		s.Format.CommaStyle = c.Format.CommaStyle
	}
	if c.Format.SingleTargetPolicy != "" {
		s.Format.SingleTargetPolicy = c.Format.SingleTargetPolicy
	}
	s.Format.AlignFollowing = c.Format.AlignFollowing
	if c.Format.NewlineAtEOF != nil {
		s.Format.NewlineAtEOF = *c.Format.NewlineAtEOF
	}
	if c.Format.MaxBlankLines > 0 {
		s.Format.MaxBlankLines = c.Format.MaxBlankLines
	}
	s.Format.CteBracketNewline = c.Format.CteBracketNewline
	if len(c.Format.Type) > 0 {
		s.Format.Types = make(map[string]TypeFormat, len(c.Format.Type))
		for k, v := range c.Format.Type {
			s.Format.Types[k] = TypeFormat{LinePosition: v.LinePosition}
		}
	}
	return s, nil
}

// IsSelected resolves rule selection.
func (s Settings) IsSelected(code, name, family, category string, def bool) bool {
	return s.Set.Selects(code, name, family, category, def)
}

// IsWarning reports whether a rule is downgraded to a warning.
func (s Settings) IsWarning(code, name, family, category string) bool {
	for _, w := range s.Warnings {
		if selector.Matches(w, code, name, family, category) {
			return true
		}
	}
	return false
}

// IsFixable reports whether the rule's fixes may be applied.
func (s Settings) IsFixable(code, name, family, category string) bool {
	fixable := true
	if len(s.Fixable) > 0 {
		fixable = false
		for _, f := range s.Fixable {
			if strings.EqualFold(f, "ALL") || selector.Matches(f, code, name, family, category) {
				fixable = true
			}
		}
	}
	for _, u := range s.Unfixable {
		if selector.Matches(u, code, name, family, category) {
			return false
		}
	}
	return fixable
}

// SafeFileExt reports whether path has a configured SQL extension.
func (s Settings) SafeFileExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range s.FileExts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// IgnoreForFile returns the per-file ignore selectors that match path.
func (s Settings) IgnoreForFile(path string) []string {
	if len(s.PerFileIgnores) == 0 {
		return nil
	}
	rel := path
	if s.ConfigDir != "" {
		if r, err := filepath.Rel(s.ConfigDir, path); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	var out []string
	for glob, sels := range s.PerFileIgnores {
		if ok, _ := doublestar.Match(glob, rel); ok {
			out = append(out, sels...)
		}
	}
	return out
}

// RuleOption returns a rule-specific option.
func (s Settings) RuleOption(ruleName, key string) (any, bool) {
	if s.Rules == nil {
		return nil, false
	}
	opts, ok := s.Rules[ruleName]
	if !ok {
		return nil, false
	}
	v, ok := opts[key]
	return v, ok
}

// RuleString returns a string rule option or def.
func (s Settings) RuleString(ruleName, key, def string) string {
	if v, ok := s.RuleOption(ruleName, key); ok {
		if sv, ok := v.(string); ok && sv != "" {
			return sv
		}
	}
	return def
}

// RuleBool returns a bool rule option or def.
func (s Settings) RuleBool(ruleName, key string, def bool) bool {
	if v, ok := s.RuleOption(ruleName, key); ok {
		if bv, ok := v.(bool); ok {
			return bv
		}
	}
	return def
}

// RuleInt returns an int rule option or def.
func (s Settings) RuleInt(ruleName, key string, def int) int {
	if v, ok := s.RuleOption(ruleName, key); ok {
		switch n := v.(type) {
		case int64:
			return int(n)
		case int:
			return n
		}
	}
	return def
}
