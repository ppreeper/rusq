// Package config loads TOML configuration, discovers it hierarchically, and
// resolves precedence.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the on-disk configuration schema. Zero values mean "not set".
type Config struct {
	Dialect            string   `toml:"dialect"`
	Templater          string   `toml:"templater"`
	LineLength         int      `toml:"line-length"`
	TargetVersion      string   `toml:"target-version"`
	SQLFileExts        []string `toml:"sql-file-exts"`
	RenderVariantLimit int      `toml:"render-variant-limit"`
	MaxParseDepth      int      `toml:"max-parse-depth"`
	MaxParseNodes      int      `toml:"max-parse-nodes"`
	Encoding           string   `toml:"encoding"`
	Include            []string `toml:"include"`
	Exclude            []string `toml:"exclude"`
	Extend             string   `toml:"extend"`

	Lint         LintConfig                `toml:"lint"`
	Format       FormatConfig              `toml:"format"`
	Rules        map[string]map[string]any `toml:"rules"`
	TemplaterCfg map[string]map[string]any `toml:"templaters"`
	Index        IndexConfig               `toml:"index"`
}

// LintConfig is the [lint] table.
type LintConfig struct {
	Select         []string            `toml:"select"`
	ExtendSelect   []string            `toml:"extend-select"`
	Ignore         []string            `toml:"ignore"`
	ExtendIgnore   []string            `toml:"extend-ignore"`
	Warnings       []string            `toml:"warnings"`
	Fixable        []string            `toml:"fixable"`
	Unfixable      []string            `toml:"unfixable"`
	ExcludeRules   []string            `toml:"exclude-rules"`
	PerFileIgnores map[string][]string `toml:"per-file-ignores"`
	Preview        bool                `toml:"preview"`
}

// FormatConfig is the [format] table.
type FormatConfig struct {
	IndentUnit          string                `toml:"indent-unit"`
	IndentWidth         int                   `toml:"indent-width"`
	LinePosition        string                `toml:"line-position"`
	KeywordLinePosition string                `toml:"keyword-line-position"`
	CommaStyle          string                `toml:"comma-style"`
	AlignFollowing      bool                  `toml:"align-following"`
	SingleTargetPolicy  string                `toml:"single-target-policy"`
	NewlineAtEOF        *bool                 `toml:"newline-at-eof"`
	MaxBlankLines       int                   `toml:"max-blank-lines"`
	CteBracketNewline   bool                  `toml:"cte-bracket-newline"`
	Type                map[string]TypeConfig `toml:"type"`
}

// TypeConfig overrides layout for one segment type. The key is a logical
// segment name such as "comma", "operator" or "keyword".
type TypeConfig struct {
	LinePosition string `toml:"line-position"`
}

// IndexConfig is the [index] table.
type IndexConfig struct {
	Enabled bool `toml:"enabled"`
}

// FileNames are the config file names, in priority order within a directory.
var FileNames = []string{"rusq.toml", ".rusq.toml"}

// Load reads one config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

// Find walks from dir upward and returns the config files found, nearest
// first.
func Find(dir string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for {
		for _, name := range FileNames {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil && !seen[p] {
				out = append(out, p)
				seen[p] = true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out, nil
}

// UserConfigPath returns the user-level config path if it exists.
func UserConfigPath() (string, bool) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(dir, "rusq", "rusq.toml")
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

// LoadChain resolves the effective config for startDir. When explicit is set,
// only that file is loaded and discovery is skipped. When isolated is true, no
// files are loaded.
func LoadChain(startDir string, explicit string, isolated bool) (*Config, []string, error) {
	if isolated {
		return &Config{}, nil, nil
	}
	if explicit != "" {
		c, err := Load(explicit)
		if err != nil {
			return nil, nil, err
		}
		return c, []string{explicit}, nil
	}

	var files []string
	if p, ok := UserConfigPath(); ok {
		files = append(files, p)
	}
	found, err := Find(startDir)
	if err != nil {
		return nil, nil, err
	}
	// found is nearest-first; apply outermost first so nearer files win.
	for i := len(found) - 1; i >= 0; i-- {
		files = append(files, found[i])
	}

	eff := &Config{}
	for _, f := range files {
		c, err := Load(f)
		if err != nil {
			return nil, files, err
		}
		if c.Extend != "" {
			ext := c.Extend
			if !filepath.IsAbs(ext) {
				ext = filepath.Join(filepath.Dir(f), ext)
			}
			outer, err := Load(ext)
			if err != nil {
				return nil, files, err
			}
			Merge(eff, outer)
		}
		Merge(eff, c)
	}
	return eff, files, nil
}

// Merge overlays src onto dst, ignoring zero values in src.
func Merge(dst, src *Config) {
	if src == nil {
		return
	}
	setStr(&dst.Dialect, src.Dialect)
	setStr(&dst.Templater, src.Templater)
	setInt(&dst.LineLength, src.LineLength)
	setStr(&dst.TargetVersion, src.TargetVersion)
	setSlice(&dst.SQLFileExts, src.SQLFileExts)
	setInt(&dst.RenderVariantLimit, src.RenderVariantLimit)
	setInt(&dst.MaxParseDepth, src.MaxParseDepth)
	setInt(&dst.MaxParseNodes, src.MaxParseNodes)
	setStr(&dst.Encoding, src.Encoding)
	setSlice(&dst.Include, src.Include)
	setSlice(&dst.Exclude, src.Exclude)

	setSlice(&dst.Lint.Select, src.Lint.Select)
	setSlice(&dst.Lint.ExtendSelect, src.Lint.ExtendSelect)
	setSlice(&dst.Lint.Ignore, src.Lint.Ignore)
	setSlice(&dst.Lint.ExtendIgnore, src.Lint.ExtendIgnore)
	setSlice(&dst.Lint.Warnings, src.Lint.Warnings)
	setSlice(&dst.Lint.Fixable, src.Lint.Fixable)
	setSlice(&dst.Lint.Unfixable, src.Lint.Unfixable)
	setSlice(&dst.Lint.ExcludeRules, src.Lint.ExcludeRules)
	if src.Lint.PerFileIgnores != nil {
		if dst.Lint.PerFileIgnores == nil {
			dst.Lint.PerFileIgnores = map[string][]string{}
		}
		for k, v := range src.Lint.PerFileIgnores {
			dst.Lint.PerFileIgnores[k] = v
		}
	}
	if src.Lint.Preview {
		dst.Lint.Preview = true
	}

	setStr(&dst.Format.IndentUnit, src.Format.IndentUnit)
	setInt(&dst.Format.IndentWidth, src.Format.IndentWidth)
	setStr(&dst.Format.LinePosition, src.Format.LinePosition)
	setStr(&dst.Format.KeywordLinePosition, src.Format.KeywordLinePosition)
	setStr(&dst.Format.CommaStyle, src.Format.CommaStyle)
	setStr(&dst.Format.SingleTargetPolicy, src.Format.SingleTargetPolicy)
	if src.Format.AlignFollowing {
		dst.Format.AlignFollowing = true
	}
	if src.Format.NewlineAtEOF != nil {
		dst.Format.NewlineAtEOF = src.Format.NewlineAtEOF
	}
	setInt(&dst.Format.MaxBlankLines, src.Format.MaxBlankLines)
	if src.Format.CteBracketNewline {
		dst.Format.CteBracketNewline = true
	}
	if src.Format.Type != nil {
		if dst.Format.Type == nil {
			dst.Format.Type = map[string]TypeConfig{}
		}
		for k, v := range src.Format.Type {
			cur := dst.Format.Type[k]
			if v.LinePosition != "" {
				cur.LinePosition = v.LinePosition
			}
			dst.Format.Type[k] = cur
		}
	}

	if src.Rules != nil {
		if dst.Rules == nil {
			dst.Rules = map[string]map[string]any{}
		}
		for k, v := range src.Rules {
			dst.Rules[k] = v
		}
	}
	if src.TemplaterCfg != nil {
		if dst.TemplaterCfg == nil {
			dst.TemplaterCfg = map[string]map[string]any{}
		}
		for k, v := range src.TemplaterCfg {
			dst.TemplaterCfg[k] = v
		}
	}
	if src.Index.Enabled {
		dst.Index.Enabled = true
	}
}

func setStr(dst *string, src string) {
	if src != "" {
		*dst = src
	}
}
func setInt(dst *int, src int) {
	if src != 0 {
		*dst = src
	}
}
func setSlice(dst *[]string, src []string) {
	if len(src) > 0 {
		*dst = src
	}
}

// ParseAssignment parses a --config KEY=VALUE override.
func ParseAssignment(s string) (key, value string, ok bool) {
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}

// ErrNotFound is returned when an explicit config file is missing.
var ErrNotFound = errors.New("config file not found")
