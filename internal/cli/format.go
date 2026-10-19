package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ppreeper/rusq/internal/formatter"
	"github.com/ppreeper/rusq/internal/output"
	"github.com/ppreeper/rusq/internal/walk"
)

var formatFlags struct {
	Check            bool
	Diff             bool
	LineLength       int
	IndentWidth      int
	IndentUnit       string
	Range            string
	ExitNonZeroOnFmt bool
}

func newFormatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "format [PATH]...",
		Short: "Format SQL files",
		RunE:  runFormat,
	}
	f := cmd.Flags()
	f.BoolVar(&formatFlags.Check, "check", false, "check formatting without writing")
	f.BoolVar(&formatFlags.Diff, "diff", false, "print a unified diff instead of writing")
	f.IntVar(&formatFlags.LineLength, "line-length", 0, "target line length")
	f.IntVar(&formatFlags.IndentWidth, "indent-width", 0, "indent width")
	f.StringVar(&formatFlags.IndentUnit, "indent-unit", "", "indent unit: space|tab")
	f.StringVar(&formatFlags.Range, "range", "", "format a line range: start:col-end:col")
	f.BoolVar(&formatFlags.ExitNonZeroOnFmt, "exit-non-zero-on-format", false, "exit 1 if a file was reformatted")
	return cmd
}

func runFormat(cmd *cobra.Command, args []string) error {
	s, err := resolveSettings(formatFlags.LineLength, formatFlags.IndentWidth)
	if err != nil {
		return err
	}
	if formatFlags.IndentUnit != "" {
		s.Format.IndentUnit = formatFlags.IndentUnit
	}
	startLine, endLine, err := parseRange(formatFlags.Range)
	if err != nil {
		return err
	}

	stdin := len(args) == 1 && args[0] == "-"
	var files []walk.File
	if stdin {
		files = []walk.File{{Path: "-", Explicit: true}}
	} else {
		res, err := discover(s, args)
		if err != nil {
			return err
		}
		files = res.Files
	}

	w, closeFn, err := outputWriter()
	if err != nil {
		return err
	}
	defer closeFn()

	changedAny := false
	var unformatted []string
	for _, f := range files {
		src, err := readSource(f.Path)
		if err != nil {
			return err
		}
		fm := formatter.New(s)
		fm.StartLine = startLine
		fm.EndLine = endLine
		out := fm.FormatBytes(src.Bytes)
		changed := !formatter.Equal(src.Bytes, out)
		if stdin {
			_, _ = w.Write(out)
			changedAny = changedAny || changed
			continue
		}
		if !changed {
			continue
		}
		changedAny = true
		switch {
		case formatFlags.Diff:
			fmt.Fprint(w, output.UnifiedDiff(f.Path, src.Bytes, out))
		case formatFlags.Check:
			unformatted = append(unformatted, f.Path)
		default:
			if err := os.WriteFile(f.Path, out, 0o644); err != nil {
				return err
			}
		}
	}
	if formatFlags.Check && len(unformatted) > 0 {
		if strings.EqualFold(G.OutputFormat, "json") {
			fmt.Fprint(w, "{\"unformatted\":[")
			for i, p := range unformatted {
				if i > 0 {
					fmt.Fprint(w, ",")
				}
				fmt.Fprintf(w, "%q", p)
			}
			fmt.Fprint(w, "]}\n")
		} else {
			for _, p := range unformatted {
				fmt.Fprintln(w, p)
			}
		}
	}
	if (formatFlags.Check && changedAny) || (formatFlags.ExitNonZeroOnFmt && changedAny) {
		return ExitError{Code: 1}
	}
	return nil
}

func parseRange(s string) (int, int, error) {
	if s == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range %q", s)
	}
	start := lineOf(parts[0])
	end := lineOf(parts[1])
	if start <= 0 || end < start {
		return 0, 0, fmt.Errorf("invalid range %q", s)
	}
	return start, end, nil
}

func lineOf(s string) int {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
}
