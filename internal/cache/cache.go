// Package cache stores lint results keyed by content and settings hashes.
package cache

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/crypto/blake2b"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/version"
)

// DirName is the default cache directory.
const DirName = ".rusq_cache"

// Cache is a content-addressed diagnostic cache.
type Cache struct {
	Dir string
}

// New returns a cache rooted at dir.
func New(dir string) *Cache {
	if dir == "" {
		dir = DirName
	}
	return &Cache{Dir: dir}
}

// Key hashes the path, source bytes, and settings hash.
func Key(path string, src []byte, settingsHash string) string {
	h, _ := blake2b.New256(nil)
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write(src)
	h.Write([]byte{0})
	h.Write([]byte(settingsHash))
	return hex.EncodeToString(h.Sum(nil))
}

// SettingsHash hashes the settings that affect diagnostics.
func SettingsHash(s settings.Settings) string {
	type snap struct {
		Dialect       string
		Templater     string
		LineLength    int
		TargetVersion string
		Include       []string
		Exclude       []string
		Select        []string
		Ignore        []string
		Warnings      []string
		Fixable       []string
		Unfixable     []string
		Preview       bool
		Rules         map[string]map[string]any
	}
	b, _ := json.Marshal(snap{
		Dialect: s.DialectName, Templater: s.Templater, LineLength: s.LineLength,
		TargetVersion: s.TargetVersion, Include: s.Include, Exclude: s.Exclude,
		Select: s.Set.Include, Ignore: s.Set.Exclude, Warnings: s.Warnings,
		Fixable: s.Fixable, Unfixable: s.Unfixable, Preview: s.Preview, Rules: s.Rules,
	})
	sum := blake2b.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type entry struct {
	Version     string                  `json:"version"`
	Diagnostics []diagnostic.Diagnostic `json:"diagnostics"`
}

// Get returns cached diagnostics for key.
func (c *Cache) Get(key string) ([]diagnostic.Diagnostic, bool) {
	p := c.path(key)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var e entry
	if err := json.Unmarshal(b, &e); err != nil || e.Version != version.Version {
		return nil, false
	}
	return e.Diagnostics, true
}

// Put stores diagnostics for key.
func (c *Cache) Put(key string, ds []diagnostic.Diagnostic) {
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(entry{Version: version.Version, Diagnostics: ds})
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}

// Clear removes the cache directory.
func (c *Cache) Clear() error {
	if err := os.RemoveAll(c.Dir); err != nil {
		return err
	}
	return nil
}

func (c *Cache) path(key string) string {
	prefix := key
	if len(prefix) > 2 {
		prefix = prefix[:2]
	}
	return filepath.Join(c.Dir, prefix, key+".json")
}
