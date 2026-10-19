package cli

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppreeper/rusq/internal/linter"
	"github.com/ppreeper/rusq/internal/output"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/source"
	"github.com/ppreeper/rusq/internal/walk"
)

var checkFlags struct {
	Fix              bool
	Unsafe           bool
	ShowFixes        bool
	Diff             bool
	FixOnly          bool
	ExitZero         bool
	ExitNonZeroOnFix bool
	Statistics       bool
	ParsingErrors    bool
	ShowFiles        bool
	ShowSettings     bool
	Watch            bool
	Preview          bool
}

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check [PATH]...",
		Short: "Lint SQL files",
		RunE:  runCheck,
	}
	f := cmd.Flags()
	f.BoolVar(&checkFlags.Fix, "fix", false, "apply fixes")
	f.BoolVar(&checkFlags.Unsafe, "unsafe-fixes", false, "include unsafe fixes")
	f.BoolVar(&checkFlags.ShowFixes, "show-fixes", false, "summarize applied fixes")
	f.BoolVar(&checkFlags.Diff, "diff", false, "print a unified diff instead of writing")
	f.BoolVar(&checkFlags.FixOnly, "fix-only", false, "apply fixes but do not report remaining diagnostics")
	f.BoolVar(&checkFlags.ExitZero, "exit-zero", false, "always exit 0 on findings")
	f.BoolVar(&checkFlags.ExitNonZeroOnFix, "exit-non-zero-on-fix", false, "exit 1 if anything was fixed")
	f.BoolVar(&checkFlags.Statistics, "statistics", false, "print per-rule counts")
	f.BoolVar(&checkFlags.ParsingErrors, "parsing-errors", false, "exit 2 on parse errors")
	f.BoolVar(&checkFlags.ShowFiles, "show-files", false, "print the selected files")
	f.BoolVar(&checkFlags.ShowSettings, "show-settings", false, "print effective settings")
	f.BoolVarP(&checkFlags.Watch, "watch", "w", false, "re-run on file changes")
	f.BoolVar(&checkFlags.Preview, "preview", false, "enable preview rules")
	return cmd
}

func runCheck(cmd *cobra.Command, args []string) error {
	start := time.Now()
	s, err := resolveSettings(0, 0)
	if err != nil {
		return err
	}
	s.Preview = s.Preview || checkFlags.Preview
	opts := linter.Options{Fix: checkFlags.Fix || checkFlags.Diff, UnsafeFixes: checkFlags.Unsafe, FixOnly: checkFlags.FixOnly}

	if checkFlags.ShowSettings {
		printSettings(cmd, s)
	}

	stdin := len(args) == 1 && args[0] == "-"
	if checkFlags.Watch {
		if stdin {
			return fmt.Errorf("--watch cannot be used with stdin")
		}
		return runCheckWatch(cmd, args, opts)
	}
	var files []walk.File
	if stdin {
		files = []walk.File{{Path: "-", Explicit: true}}
	} else {
		res, err := discover(s, args)
		if err != nil {
			return err
		}
		files = res.Files
		if checkFlags.ShowFiles {
			for _, f := range files {
				fmt.Fprintln(cmd.OutOrStdout(), f.Path)
			}
			for _, sk := range res.Skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "skip %s (%s)\n", sk.Path, sk.Reason)
			}
			return nil
		}
	}

	if checkFlags.Diff {
		return runCheckDiff(s, files, opts)
	}

	results, changed, err := lintFiles(s, files, opts, checkFlags.Fix)
	if err != nil {
		return err
	}
	if err := report(results); err != nil {
		return err
	}
	errors, warnings, hints := output.Counts(results)
	if checkFlags.Statistics {
		for code, n := range output.Statistics(results) {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s\t%d\n", code, n)
		}
	}
	if checkFlags.ShowFixes && changed {
		fmt.Fprintf(cmd.ErrOrStderr(), "rusq: fixes applied\n")
	}
	if G.Verbose {
		fmt.Fprintf(cmd.ErrOrStderr(), "rusq: %d files, %d errors, %d warnings, %d hints in %s\n",
			len(results), errors, warnings, hints, time.Since(start).Round(time.Millisecond))
	}
	if checkFlags.ParsingErrors && hasParseError(results) {
		return ExitError{Code: 2}
	}
	if checkFlags.ExitZero {
		return nil
	}
	if checkFlags.ExitNonZeroOnFix && changed {
		return ExitError{Code: 1}
	}
	if errors+warnings > 0 {
		return ExitError{Code: 1}
	}
	return nil
}

func runCheckDiff(s settings.Settings, files []walk.File, opts linter.Options) error {
	w, closeFn, err := outputWriter()
	if err != nil {
		return err
	}
	defer closeFn()
	changedAny := false
	var results []output.FileResult
	for _, f := range files {
		src, err := readSource(f.Path)
		if err != nil {
			return err
		}
		l := linter.New(s)
		res := l.Run(src, opts)
		if res.Changed && len(res.FixedSource) > 0 {
			changedAny = true
			fmt.Fprint(w, output.UnifiedDiff(f.Path, src.Bytes, res.FixedSource))
			res = l.Run(source.New(f.Path, res.FixedSource), linter.Options{})
			results = append(results, output.FileResult{Path: f.Path, Locator: source.NewLocator(res.FixedSource), Diagnostics: res.Diagnostics})
		} else {
			results = append(results, output.FileResult{Path: f.Path, Locator: source.NewLocator(src.Bytes), Diagnostics: res.Diagnostics})
		}
	}
	if changedAny {
		return ExitError{Code: 1}
	}
	errors, warnings, _ := output.Counts(results)
	if errors+warnings > 0 {
		return ExitError{Code: 1}
	}
	return nil
}

// watchInterval is how often watch mode re-checks the file set.
const watchInterval = 250 * time.Millisecond

// fileStamp is a cheap change fingerprint for one file.
type fileStamp struct {
	path string
	size int64
	mod  time.Time
}

// runCheckWatch lints, reports, and then waits for the file set on disk to
// change before linting again. Ctrl-C exits cleanly.
func runCheckWatch(cmd *cobra.Command, args []string, opts linter.Options) error {
	errOut := cmd.ErrOrStderr()
	fmt.Fprintf(errOut, "rusq: watching for changes (Ctrl-C to stop)\n")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	defer signal.Stop(stop)

	for {
		s, err := resolveSettings(0, 0)
		if err != nil {
			return err
		}
		s.Preview = s.Preview || checkFlags.Preview

		res, err := discover(s, args)
		if err != nil {
			return err
		}
		before := snapshotFiles(res.Files)

		if isTTY() && !G.Quiet {
			fmt.Fprint(cmd.OutOrStdout(), "\033[H\033[2J")
		}
		if checkFlags.Diff {
			if err := runCheckDiff(s, res.Files, opts); err != nil {
				if _, ok := err.(ExitError); !ok {
					return err
				}
			}
		} else {
			results, _, err := lintFiles(s, res.Files, opts, checkFlags.Fix)
			if err != nil {
				return err
			}
			if err := report(results); err != nil {
				return err
			}
			errors, warnings, hints := output.Counts(results)
			fmt.Fprintf(errOut, "rusq: %d files — %d errors, %d warnings, %d hints\n",
				len(res.Files), errors, warnings, hints)
		}

		again, err := waitForChange(s, args, before, stop)
		if err != nil {
			return err
		}
		if !again {
			fmt.Fprintf(errOut, "rusq: stopped\n")
			return nil
		}
	}
}

// waitForChange polls the file set until it differs from before, or until the
// process is interrupted. It returns false when interrupted.
func waitForChange(s settings.Settings, args []string, before []fileStamp, stop <-chan os.Signal) (bool, error) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return false, nil
		case <-ticker.C:
			res, err := discover(s, args)
			if err != nil {
				return false, err
			}
			if stampsDiffer(before, snapshotFiles(res.Files)) {
				return true, nil
			}
		}
	}
}

// snapshotFiles fingerprints the current file set (path, size, mtime).
func snapshotFiles(files []walk.File) []fileStamp {
	stamps := make([]fileStamp, 0, len(files))
	for _, f := range files {
		st := fileStamp{path: f.Path}
		if info, err := os.Stat(f.Path); err == nil && info != nil {
			st.size = info.Size()
			st.mod = info.ModTime()
		}
		stamps = append(stamps, st)
	}
	return stamps
}

func stampsDiffer(a, b []fileStamp) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].path != b[i].path || a[i].size != b[i].size || !a[i].mod.Equal(b[i].mod) {
			return true
		}
	}
	return false
}

// isTTY reports whether stdout is a terminal, so watch mode only clears the
// screen when a human is watching.
func isTTY() bool {
	if G.OutputFile != "" || os.Getenv("RUSQ_OUTPUT_FILE") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func hasParseError(results []output.FileResult) bool {
	for _, f := range results {
		for _, d := range f.Diagnostics {
			if d.Code == "RUS104" {
				return true
			}
		}
	}
	return false
}

func printSettings(cmd *cobra.Command, s settings.Settings) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "dialect = %q\n", s.DialectName)
	fmt.Fprintf(out, "templater = %q\n", s.Templater)
	fmt.Fprintf(out, "line-length = %d\n", s.LineLength)
	fmt.Fprintf(out, "files = %v\n", s.FileExts)
	if len(s.Set.Include) > 0 {
		fmt.Fprintf(out, "select = %v\n", s.Set.Include)
	}
	if len(s.Set.Exclude) > 0 {
		fmt.Fprintf(out, "ignore = %v\n", s.Set.Exclude)
	}
	if len(s.ConfigFiles) > 0 {
		fmt.Fprintf(out, "config = %v\n", s.ConfigFiles)
	}
}
