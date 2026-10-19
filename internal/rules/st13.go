package rules

import (
	"path/filepath"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lint"
	"github.com/ppreeper/rusq/internal/source"
)

// ST13 — the same object is defined in more than one file.
//
// This is a cross-file rule (SPEC §8.3): it only reports when the project index
// is available, and it reports once per duplicate — on every defining file
// except the first in path order.
type st13 struct{ meta Meta }

func (r *st13) Meta() Meta { return r.meta }
func (r *st13) Check(ctx *lint.Context) []diagnostic.Diagnostic {
	if ctx.Index == nil || ctx.AST == nil {
		return nil
	}
	self := filepath.ToSlash(ctx.Source.Path)
	var out []diagnostic.Diagnostic
	for _, def := range ctx.Index.DefinitionsInPath(self) {
		if def.Kind == "cte" {
			continue // CTE names are statement-local; duplicates are not a project error
		}
		files := ctx.Index.DefinitionFiles(def.Name)
		if len(files) < 2 {
			continue
		}
		if files[0] == self {
			continue // the first definition is the canonical one
		}
		out = append(out, diag(r.meta, source.Range{Start: def.Start, End: def.End},
			"Object "+def.Name+" is also defined in "+files[0]+"."))
	}
	return out
}

func init() {
	Register(&st13{meta: newMeta("ST13", "structure.duplicate_definition", "structure", Suspicious, Index, diagnostic.NoFix, "Object is defined in more than one file.")})
}
