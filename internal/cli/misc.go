package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppreeper/rusq/internal/cache"
	"github.com/ppreeper/rusq/internal/lsp"
)

func newInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter rusq.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "rusq.toml"
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to overwrite)", path)
			}
			content := `dialect = "ansi"
templater = "raw"
line-length = 88

[lint]
# select = ["core"]            # default: correctness, suspicious, security
# extend-select = ["LT", "CP"]
# ignore = []
# warnings = []

[format]
indent-unit = "space"
indent-width = 4
line-position = "trailing"
keyword-line-position = "leading"

[index]
enabled = false
`
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")
	return cmd
}

func newCleanCmd() *cobra.Command {
	var cacheDir string
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Clear the cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := cacheDir
			if dir == "" {
				dir = G.CacheDir
			}
			if dir == "" {
				dir = cache.DirName
			}
			c := cache.New(dir)
			if err := c.Clear(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "cleared %s\n", dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "cache directory")
	return cmd
}

func newServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the language server (LSP over stdio)",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := lsp.Options{
				Config:    G.Config,
				Isolated:  G.Isolated,
				Dialect:   G.Dialect,
				Templater: G.Templater,
				CacheDir:  G.CacheDir,
				NoCache:   G.NoCache,
			}
			if wd, err := os.Getwd(); err == nil {
				opts.RootDir = wd
			}
			return lsp.Serve(os.Stdin, os.Stdout, opts)
		},
	}
}

func newCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate-shell-completion <bash|zsh|fish|powershell>",
		Short: "Generate a shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
	return cmd
}
