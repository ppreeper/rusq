package formatest

import (
	"flag"
	"path/filepath"
	"testing"
)

// update rewrites golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite golden files")

// formatRoot is the fixture tree relative to this package.
const formatRoot = "../../testdata/format"

func TestFormatFixtures(t *testing.T) {
	cases := Discover(t, formatRoot)
	if len(cases) == 0 {
		t.Fatalf("no fixtures found under %s", filepath.Clean(formatRoot))
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			RunCase(t, c, *update)
		})
	}
}
