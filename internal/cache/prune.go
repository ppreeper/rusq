package cache

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Prune evicts the least recently used cache entries until the directory
// occupies at most maxBytes, then removes any directories left empty. A
// maxBytes <= 0 disables the cap. Because the cache is only a hint, evicting
// entries never changes results — only the next run's speed.
//
// Recency is the entry's modification time: Get reads an entry, Put rewrites
// it, so a file that keeps producing the same result keeps its age while a
// changed file gets a fresh entry.
func (c *Cache) Prune(maxBytes int64) (int, error) {
	if maxBytes <= 0 || c == nil || c.Dir == "" {
		return 0, nil
	}
	type fileInfo struct {
		path string
		size int64
		mod  int64 // unix nanos for stable sorting
	}
	var files []fileInfo
	var total int64
	err := filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // a cache that cannot be read is not a failure of the run
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, fileInfo{path: p, size: info.Size(), mod: info.ModTime().UnixNano()})
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	if total <= maxBytes {
		return 0, nil
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mod != files[j].mod {
			return files[i].mod < files[j].mod
		}
		return files[i].path < files[j].path
	})
	removed := 0
	for _, f := range files {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			continue
		}
		total -= f.size
		removed++
	}
	c.removeEmptyDirs()
	return removed, nil
}

// removeEmptyDirs deletes directories left behind by eviction, deepest first.
func (c *Cache) removeEmptyDirs() {
	var dirs []string
	_ = filepath.WalkDir(c.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == c.Dir {
			return nil
		}
		dirs = append(dirs, p)
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs {
		_ = os.Remove(d) // fails harmlessly when the directory is not empty
	}
}
