// Package cli implements the rusq command-line interface.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/version"
)

// Global holds flags shared by all subcommands.
type Global struct {
	Color              string
	Config             string
	Isolated           bool
	Dialect            string
	Templater          string
	NoCache            bool
	CacheDir           string
	Workers            int
	Quiet              bool
	Verbose            bool
	Silent             bool
	OutputFormat       string
	OutputFile         string
	StdinFilename      string
	Select             []string
	ExtendSelect       []string
	Ignore             []string
	ExtendIgnore       []string
	PerFileIgnores     []string
	Fixable            []string
	Unfixable          []string
	Warnings           []string
	IgnoreSuppressions bool
	DisableNoqa        bool
	Exclude            []string
	ExtendExclude      []string
	ForceExclude       bool
	NoRespectGitignore bool
}

// Flag values are bound here.
var G Global

// ExitError carries an exit code.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// NewRootCmd builds the command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "rusq [PATH]...",
		Short:         "An extremely fast SQL linter and formatter",
		Long:          "rusq lints and formats SQL. With no subcommand it behaves as `rusq check`.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, args)
		},
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	pf := root.PersistentFlags()
	pf.StringVar(&G.Color, "color", "auto", "colour: auto|always|never")
	pf.StringVar(&G.Config, "config", "", "pin a config file")
	pf.BoolVar(&G.Isolated, "isolated", false, "ignore all config files")
	pf.StringVar(&G.Dialect, "dialect", "", "SQL dialect")
	pf.StringVar(&G.Templater, "templater", "", "templater: raw|placeholder|jinja|python")
	pf.BoolVarP(&G.NoCache, "no-cache", "n", false, "disable the result cache")
	pf.StringVar(&G.CacheDir, "cache-dir", "", "cache directory (default .rusq_cache)")
	pf.IntVar(&G.Workers, "workers", 0, "maximum parallel workers (default: number of CPUs)")
	pf.BoolVarP(&G.Quiet, "quiet", "q", false, "hide the fix summary")
	pf.BoolVarP(&G.Verbose, "verbose", "v", false, "write a timing summary to stderr")
	pf.BoolVarP(&G.Silent, "silent", "s", false, "silence stdout")
	pf.StringVar(&G.OutputFormat, "output-format", "concise", "output format")
	pf.StringVarP(&G.OutputFile, "output-file", "o", "", "write the report to a file")
	pf.StringVar(&G.StdinFilename, "stdin-filename", "", "logical path when reading from stdin")
	pf.StringSliceVar(&G.Select, "select", nil, "rules to enable")
	pf.StringSliceVar(&G.ExtendSelect, "extend-select", nil, "rules to add")
	pf.StringSliceVar(&G.Ignore, "ignore", nil, "rules to disable")
	pf.StringSliceVar(&G.ExtendIgnore, "extend-ignore", nil, "rules to add to the ignore set")
	pf.StringSliceVar(&G.PerFileIgnores, "per-file-ignores", nil, "GLOB:CODE[,CODE...]")
	pf.StringSliceVar(&G.Fixable, "fixable", nil, "rules whose fixes may be applied")
	pf.StringSliceVar(&G.Unfixable, "unfixable", nil, "rules whose fixes may not be applied")
	pf.StringSliceVar(&G.Warnings, "warnings", nil, "rules downgraded to warnings")
	pf.BoolVar(&G.IgnoreSuppressions, "ignore-suppressions", false, "ignore all suppression directives")
	pf.BoolVar(&G.DisableNoqa, "disable-noqa", false, "ignore all noqa directives")
	pf.StringSliceVar(&G.Exclude, "exclude", nil, "paths to exclude")
	pf.StringSliceVar(&G.ExtendExclude, "extend-exclude", nil, "paths to add to the exclude set")
	pf.BoolVar(&G.ForceExclude, "force-exclude", false, "apply excludes to explicit paths")
	pf.BoolVar(&G.NoRespectGitignore, "no-respect-gitignore", false, "ignore .gitignore files")

	root.AddCommand(
		newCheckCmd(),
		newFormatCmd(),
		newParseCmd(),
		newRuleCmd(),
		newConfigCmd(),
		newDialectsCmd(),
		newTemplatersCmd(),
		newInitCmd(),
		newCleanCmd(),
		newServerCmd(),
		newVersionCmd(),
		newCompletionCmd(root),
	)
	return root
}

// Execute runs the CLI and returns a process exit code.
func Execute() int {
	cmd := NewRootCmd()
	if err := cmd.Execute(); err != nil {
		if e, ok := err.(ExitError); ok {
			return e.Code
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	return 0
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "rusq %s\n", version.String())
		},
	}
}

func resolveSettings(lineLength int, indentWidth int) (settings.Settings, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return settings.Settings{}, err
	}
	c, files, err := loadConfig(cwd)
	if err != nil {
		return settings.Settings{}, err
	}
	s, err := settings.FromConfig(c, cwd, files)
	if err != nil {
		return settings.Settings{}, err
	}
	if G.Dialect != "" {
		d, ok := dialect.Lookup(G.Dialect)
		if !ok {
			return s, fmt.Errorf("unknown dialect %q", G.Dialect)
		}
		s.DialectName = d.Name
		s.Dialect = d
	}
	if G.Templater != "" {
		s.Templater = G.Templater
	}
	if len(G.Select) > 0 {
		s.Set.Include = append([]string{}, G.Select...)
	}
	s.Set.Include = append(s.Set.Include, G.ExtendSelect...)
	s.Set.Exclude = append(s.Set.Exclude, G.Ignore...)
	s.Set.Exclude = append(s.Set.Exclude, G.ExtendIgnore...)
	if len(G.Warnings) > 0 {
		s.Warnings = append(s.Warnings, G.Warnings...)
	}
	if len(G.Fixable) > 0 {
		s.Fixable = G.Fixable
	}
	if len(G.Unfixable) > 0 {
		s.Unfixable = append(s.Unfixable, G.Unfixable...)
	}
	if len(G.PerFileIgnores) > 0 {
		if s.PerFileIgnores == nil {
			s.PerFileIgnores = map[string][]string{}
		}
		for _, spec := range G.PerFileIgnores {
			if glob, codes := splitPerFile(spec); glob != "" {
				s.PerFileIgnores[glob] = append(s.PerFileIgnores[glob], codes...)
			}
		}
	}
	s.IgnoreSuppressions = G.IgnoreSuppressions
	s.DisableNoqa = G.DisableNoqa
	if lineLength > 0 {
		s.LineLength = lineLength
		s.Format.LineLength = lineLength
	}
	if indentWidth > 0 {
		s.Format.IndentWidth = indentWidth
	}
	return s, nil
}

func splitPerFile(spec string) (string, []string) {
	i := -1
	for j := 0; j < len(spec); j++ {
		if spec[j] == ':' {
			i = j
			break
		}
	}
	if i <= 0 {
		return "", nil
	}
	glob := spec[:i]
	var codes []string
	cur := ""
	for _, r := range spec[i+1:] {
		if r == ',' || r == ' ' {
			if cur != "" {
				codes = append(codes, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		codes = append(codes, cur)
	}
	return glob, codes
}
