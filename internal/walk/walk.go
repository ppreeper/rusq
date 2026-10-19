// Package walk discovers SQL files, honouring include/exclude globs and
// gitignore-style ignore files.
package walk

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Options controls discovery.
type Options struct {
	Roots            []string
	Exts             []string
	Include          []string
	Exclude          []string
	RespectGitignore bool
	ForceExclude     bool
}

// File is a discovered file.
type File struct {
	Path     string
	Explicit bool
}

// Skipped records a path that was not selected and why.
type Skipped struct {
	Path   string
	Reason string
}

// Result is the outcome of discovery.
type Result struct {
	Files   []File
	Skipped []Skipped
}

// Discover walks the roots and returns matching files.
func Discover(opts Options) (Result, error) {
	var res Result
	for _, root := range opts.Roots {
		info, err := os.Stat(root)
		if err != nil {
			return res, err
		}
		if !info.IsDir() {
			if !opts.ForceExclude && !matchesAny(opts.Exclude, root) {
				res.Files = append(res.Files, File{Path: root, Explicit: true})
				continue
			}
			if matchesAny(opts.Exclude, root) {
				res.Skipped = append(res.Skipped, Skipped{Path: root, Reason: "excluded"})
			}
			continue
		}
		patterns := loadIgnoreFile(filepath.Join(root, ".gitignore"), filepath.Join(root, ".ignore"))
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				res.Skipped = append(res.Skipped, Skipped{Path: path, Reason: err.Error()})
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if name == ".git" || name == ".rusq_cache" || name == "node_modules" {
					return filepath.SkipDir
				}
				if opts.RespectGitignore && ignored(patterns, relPath(root, path), true) {
					return filepath.SkipDir
				}
				return nil
			}
			rel := relPath(root, path)
			if opts.RespectGitignore && ignored(patterns, rel, false) {
				res.Skipped = append(res.Skipped, Skipped{Path: path, Reason: "gitignored"})
				return nil
			}
			if !hasExt(path, opts.Exts) {
				return nil
			}
			if matchesAny(opts.Exclude, rel) {
				res.Skipped = append(res.Skipped, Skipped{Path: path, Reason: "excluded"})
				return nil
			}
			if len(opts.Include) > 0 && !matchesAny(opts.Include, rel) {
				res.Skipped = append(res.Skipped, Skipped{Path: path, Reason: "not included"})
				return nil
			}
			res.Files = append(res.Files, File{Path: path})
			return nil
		})
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func relPath(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

func hasExt(path string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

func matchesAny(globs []string, path string) bool {
	p := filepath.ToSlash(path)
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
	}
	return false
}

type ignorePattern struct {
	pattern string
	negate  bool
	dirOnly bool
	rooted  bool
}

func loadIgnoreFile(paths ...string) []ignorePattern {
	var out []ignorePattern
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			pat := ignorePattern{}
			if strings.HasPrefix(line, "!") {
				pat.negate = true
				line = line[1:]
			}
			if strings.HasPrefix(line, "/") {
				pat.rooted = true
				line = line[1:]
			}
			if strings.HasSuffix(line, "/") {
				pat.dirOnly = true
				line = strings.TrimSuffix(line, "/")
			}
			pat.pattern = line
			out = append(out, pat)
		}
	}
	return out
}

func ignored(patterns []ignorePattern, rel string, isDir bool) bool {
	matched := false
	for _, p := range patterns {
		if p.dirOnly && !isDir {
			continue
		}
		ok := false
		if p.rooted {
			ok, _ = doublestar.Match(p.pattern, rel)
		} else {
			ok, _ = doublestar.Match(p.pattern, rel)
			if !ok {
				ok, _ = doublestar.Match("**/"+p.pattern, rel)
			}
		}
		if ok {
			matched = !p.negate
		}
	}
	return matched
}
