// Command rusq is an extremely fast SQL linter and formatter.
package main

import (
	"os"

	"github.com/ppreeper/rusq/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
