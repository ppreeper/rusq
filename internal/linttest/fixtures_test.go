package linttest

import (
	"flag"
	"path/filepath"
	"testing"
)

// update rewrites golden files instead of comparing against them.
var update = flag.Bool("update", false, "rewrite golden files")

// rulesRoot is the fixture tree relative to this package.
const rulesRoot = "../../testdata/rules"

func TestRuleFixtures(t *testing.T) {
	cases := DiscoverRules(t, rulesRoot)
	if len(cases) == 0 {
		t.Fatalf("no fixtures found under %s", filepath.Clean(rulesRoot))
	}
	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			RunRuleCase(t, c, *update)
		})
	}
}
