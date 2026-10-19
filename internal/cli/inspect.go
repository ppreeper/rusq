package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ppreeper/rusq/internal/dialect"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/parser"
	"github.com/ppreeper/rusq/internal/rules"
	"github.com/ppreeper/rusq/internal/tree"
)

var parseFlags struct {
	Format string
}

func newParseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parse [PATH]...",
		Short: "Dump the parse tree",
		RunE:  runParse,
	}
	cmd.Flags().StringVar(&parseFlags.Format, "format", "pretty", "json|pretty|none")
	return cmd
}

type parseNode struct {
	Kind     string      `json:"kind"`
	Text     string      `json:"text,omitempty"`
	Start    int         `json:"start"`
	End      int         `json:"end"`
	Children []parseNode `json:"children,omitempty"`
}

func toParseNode(n *tree.Node) parseNode {
	pn := parseNode{Kind: n.Kind.String(), Start: n.Span().Start, End: n.Span().End}
	if n.Token != nil {
		pn.Text = n.Token.Text
		return pn
	}
	for _, c := range n.Children {
		pn.Children = append(pn.Children, toParseNode(c))
	}
	return pn
}

func runParse(cmd *cobra.Command, args []string) error {
	s, err := resolveSettings(0, 0)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		args = []string{"-"}
	}
	for _, path := range args {
		src, err := readSource(path)
		if err != nil {
			return err
		}
		tokens, lexErrs := lexer.Lex(src, s.Dialect)
		tr, parseErrs := parser.New().Parse(tokens, s.Dialect)
		if parseFlags.Format == "none" {
			continue
		}
		root := toParseNode(tr.Root)
		switch strings.ToLower(parseFlags.Format) {
		case "json":
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(root); err != nil {
				return err
			}
		case "pretty":
			printParseNode(cmd, root, 0)
		default:
			return fmt.Errorf("unknown parse format %q", parseFlags.Format)
		}
		for _, e := range append(lexErrs, parseErrs...) {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s:%d: %s\n", path, e.Span.Start, e.Message)
		}
	}
	return nil
}

func printParseNode(cmd *cobra.Command, n parseNode, depth int) {
	indent := strings.Repeat("  ", depth)
	if n.Text != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s%s %q\n", indent, n.Kind, n.Text)
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s%s\n", indent, n.Kind)
	for _, c := range n.Children {
		printParseNode(cmd, c, depth+1)
	}
}

func newRuleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rule [CODE|SELECTOR]",
		Short: "Explain or list rules",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				for _, r := range rules.All() {
					m := r.Meta()
					def := "off"
					if m.Default {
						def = "on"
					}
					fmt.Fprintf(out, "%-6s %-32s %-12s %-8s %s\n", m.Code, m.Name, m.Category, def, m.Description)
				}
				return nil
			}
			sel := args[0]
			found := false
			for _, r := range rules.All() {
				m := r.Meta()
				if strings.EqualFold(m.Code, sel) || strings.EqualFold(m.Name, sel) ||
					strings.EqualFold(m.Family, sel) || strings.EqualFold(string(m.Category), sel) ||
					(len(sel) == 2 && strings.HasPrefix(m.Code, strings.ToUpper(sel))) {
					found = true
					fmt.Fprintf(out, "%s  %s\n", m.Code, m.Name)
					fmt.Fprintf(out, "  category:  %s\n", m.Category)
					fmt.Fprintf(out, "  severity:  %s\n", m.Severity)
					fmt.Fprintf(out, "  source:    %s\n", m.Source)
					fmt.Fprintf(out, "  fixable:   %s\n", m.Fixable)
					fmt.Fprintf(out, "  default:   %v\n", m.Default)
					fmt.Fprintf(out, "  %s\n", m.Description)
					if len(m.ConfigKeys) > 0 {
						fmt.Fprintf(out, "  options:   %s\n", strings.Join(m.ConfigKeys, ", "))
					}
					fmt.Fprintln(out)
				}
			}
			if !found {
				return fmt.Errorf("unknown rule %q", sel)
			}
			return nil
		},
	}
}

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config [KEY]",
		Short: "Show effective configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := resolveSettings(0, 0)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				printSettings(cmd, s)
				return nil
			}
			switch strings.ToLower(args[0]) {
			case "dialect":
				fmt.Fprintln(cmd.OutOrStdout(), s.DialectName)
			case "templater":
				fmt.Fprintln(cmd.OutOrStdout(), s.Templater)
			case "line-length":
				fmt.Fprintln(cmd.OutOrStdout(), s.LineLength)
			case "files", "sql-file-exts":
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(s.FileExts, " "))
			default:
				return fmt.Errorf("unknown config key %q", args[0])
			}
			return nil
		},
	}
}

func newDialectsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dialects",
		Short: "List supported dialects",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			for _, d := range dialect.All() {
				var quotes []string
				for _, q := range d.IdentQuote {
					quotes = append(quotes, string(q.Open)+"…"+string(q.Close))
				}
				parent := d.Parent
				if parent == "" {
					parent = "-"
				}
				fmt.Fprintf(out, "%-12s parent=%-10s identifiers=%s\n", d.Name, parent, strings.Join(quotes, " "))
			}
			return nil
		},
	}
}

func newTemplatersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "templaters",
		Short: "List supported templaters",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "raw          pass-through (default)")
			fmt.Fprintln(out, "placeholder  named/numbered/positional parameters")
			fmt.Fprintln(out, "jinja        Jinja2 via the native Go pongo2 engine")
			fmt.Fprintln(out, "python       best-effort brace/format rendering")
			fmt.Fprintln(out, "dbt          jinja + dbt builtins (degrade when dbt-core is absent)")
			fmt.Fprintln(out, "sqlmesh      jinja + SQLMesh conventions (degrade when SQLMesh is absent)")
			return nil
		},
	}
}
