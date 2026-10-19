package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/ppreeper/rusq/internal/cache"
	"github.com/ppreeper/rusq/internal/config"
	"github.com/ppreeper/rusq/internal/index"
	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/output"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/walk"
)

func loadConfig(cwd string) (*config.Config, []string, error) {
	if G.Config != "" {
		if _, err := os.Stat(G.Config); err == nil {
			return config.LoadChain(cwd, G.Config, G.Isolated)
		}
		if key, value, ok := config.ParseAssignment(G.Config); ok {
			c, files, err := config.LoadChain(cwd, "", G.Isolated)
			if err != nil {
				return nil, nil, err
			}
			applyAssignment(c, key, value)
			return c, files, nil
		}
		return nil, nil, fmt.Errorf("config file not found: %s", G.Config)
	}
	return config.LoadChain(cwd, "", G.Isolated)
}

func applyAssignment(c *config.Config, key, value string) {
	switch strings.ToLower(key) {
	case "dialect":
		c.Dialect = value
	case "templater":
		c.Templater = value
	case "line-length":
		if n, err := strconv.Atoi(value); err == nil {
			c.LineLength = n
		}
	case "target-version":
		c.TargetVersion = value
	}
}

func discover(s settings.Settings, roots []string) (walk.Result, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}
	exclude := append(append([]string{}, s.Exclude...), G.Exclude...)
	exclude = append(exclude, G.ExtendExclude...)
	return walk.Discover(walk.Options{
		Roots:            roots,
		Exts:             s.FileExts,
		Include:          s.Include,
		Exclude:          exclude,
		RespectGitignore: !G.NoRespectGitignore,
		ForceExclude:     G.ForceExclude,
	})
}

func readSource(path string) (source.Text, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return source.Text{}, err
		}
		name := G.StdinFilename
		if name == "" {
			name = "stdin.sql"
		}
		return source.New(name, b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return source.Text{}, err
	}
	return source.New(path, b), nil
}

func outputWriter() (io.Writer, func(), error) {
	path := G.OutputFile
	if path == "" {
		path = os.Getenv("RUSQ_OUTPUT_FILE")
	}
	if path == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

func report(results []output.FileResult) error {
	w, closeFn, err := outputWriter()
	if err != nil {
		return err
	}
	defer closeFn()
	rep := &output.Reporter{Format: G.OutputFormat, Writer: w}
	return rep.Report(results)
}

// lintFiles is the shared check pipeline. Files are linted by a bounded worker
// pool, then merged in input order so output is deterministic.
func lintFiles(s settings.Settings, files []walk.File, opts linter.Options, write bool) ([]output.FileResult, bool, error) {
	settingsHash := cache.SettingsHash(s)
	c := cache.New(resolvedCacheDir())
	if s.Index && opts.Index == nil && !hasStdin(files) {
		if ix, err := index.Build(files, index.Options{
			Settings: s,
			CacheDir: resolvedCacheDir(),
			NoCache:  G.NoCache,
			Workers:  resolvedWorkers(),
		}); err == nil {
			opts.Index = ix
			if G.Verbose {
				fmt.Fprintf(os.Stderr, "rusq: indexed %d files\n", ix.Len())
			}
		} else if G.Verbose {
			fmt.Fprintf(os.Stderr, "rusq: index: %v\n", err)
		}
	}
	results := make([]output.FileResult, len(files))
	changed := make([]bool, len(files))
	errs := make([]error, len(files))

	workers := resolvedWorkers()
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func(i int, f walk.File) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], changed[i], errs[i] = lintOneFile(s, f, opts, write, settingsHash, c)
		}(i, f)
	}
	wg.Wait()

	changedAny := false
	for i := range changed {
		if changed[i] {
			changedAny = true
		}
	}
	for _, err := range errs {
		if err != nil {
			return results, changedAny, err
		}
	}
	if !G.NoCache {
		// Evict the least recently used entries when the cache is over budget
		// ([cache] max-size, default 512MiB).
		c.Prune(s.CacheMaxSize)
	}
	return results, changedAny, nil
}

// resolvedWorkers returns the parallel worker limit.
func resolvedWorkers() int {
	if G.Workers > 0 {
		return G.Workers
	}
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return n
}

// resolvedCacheDir returns the configured cache directory, or the default.
func resolvedCacheDir() string {
	if G.CacheDir != "" {
		return G.CacheDir
	}
	return cache.DirName
}

// hasStdin reports whether any selected file is the stdin pseudo-path.
func hasStdin(files []walk.File) bool {
	for _, f := range files {
		if f.Path == "-" {
			return true
		}
	}
	return false
}

func lintOneFile(s settings.Settings, f walk.File, opts linter.Options, write bool, settingsHash string, c *cache.Cache) (output.FileResult, bool, error) {
	src, err := readSource(f.Path)
	if err != nil {
		return output.FileResult{Path: f.Path, Locator: source.NewLocator(nil)}, false, nil
	}
	displayPath := f.Path
	if displayPath == "-" {
		displayPath = src.Path
	}
	loc := source.NewLocator(src.Bytes)

	if f.Path == "-" {
		l := linter.New(s)
		res := l.Run(src, opts)
		if opts.Fix && len(res.FixedSource) > 0 {
			_, _ = os.Stdout.Write(res.FixedSource)
			return output.FileResult{Path: displayPath, Locator: source.NewLocator(res.FixedSource)}, true, nil
		}
		return output.FileResult{Path: displayPath, Locator: loc, Diagnostics: res.Diagnostics}, false, nil
	}

	if !opts.Fix && !G.NoCache {
		key := cache.Key(f.Path, src.Bytes, settingsHash)
		if cached, ok := c.Get(key); ok {
			return output.FileResult{Path: f.Path, Locator: loc, Diagnostics: cached}, false, nil
		}
		l := linter.New(s)
		res := l.Run(src, opts)
		c.Put(key, res.Diagnostics)
		return output.FileResult{Path: f.Path, Locator: loc, Diagnostics: res.Diagnostics}, false, nil
	}

	l := linter.New(s)
	res := l.Run(src, opts)
	changed := false
	if res.Changed && len(res.FixedSource) > 0 && res.FixedSource != nil {
		fixed := res.FixedSource
		if write {
			if err := os.WriteFile(f.Path, fixed, 0o644); err != nil {
				return output.FileResult{Path: f.Path, Locator: loc}, false, err
			}
		}
		changed = true
		// Re-lint after fixing so the report reflects what remains.
		res = l.Run(source.New(f.Path, fixed), linter.Options{})
		loc = source.NewLocator(fixed)
	}
	return output.FileResult{Path: f.Path, Locator: loc, Diagnostics: res.Diagnostics}, changed, nil
}
