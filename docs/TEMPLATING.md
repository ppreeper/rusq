# rusq — Templating

> How rusq turns templated SQL into something the lexer and parser can read — and
> maps every diagnostic back to the line you actually wrote.

Status: **draft v0.1** — companion to [`SPEC.md`](./SPEC.md) §9. Sections marked
**[decision]** record a choice made here; **[open]** are unresolved and need
review before implementation.

Companion documents:

- [`SPEC.md`](./SPEC.md) — the technical specification; §9 summarizes this file.
- [`PRD.md`](./PRD.md) — the product requirements (§8.5 templating).
- [`EDITOR.md`](./EDITOR.md) — editor (LSP), CI, and pre-commit integration.
- [`PERFORMANCE.md`](./PERFORMANCE.md) — budgets, including the cost bounds on
  variant rendering.

---

## 1. The problem

A file that uses Jinja is **not** parseable SQL:

```sql
SELECT
  {{ dbt_utils.star(ref('orders')) }}
FROM {{ ref('orders') }}
{% if is_incremental() %}
WHERE updated_at > (SELECT max(updated_at) FROM {{ this }})
{% endif %}
```

The lexer and parser in SPEC §12 cannot consume `{{ }}` or `{% %}`. The
templater stage therefore has two jobs:

1. **Render** the template into real SQL the pipeline can parse.
2. **Map** every byte of the rendered SQL back to a byte range in the original
   file, so diagnostics and fixes point at the right source position.

A third, subtler job follows from the first: a single render only exercises the
branches it happened to take. To lint code inside untaken `{% if %}` arms and
inside `{% for %}` bodies, the templater must produce **several render variants**
and combine their findings.

**[decision]** The templater stage runs before lexing, always. The parser never
sees a template — only real SQL from a variant.

## 2. Pipeline

```
source bytes
  → pre-scan        split into literal / templated / block / comment slices
  → render          Jinja engine executes the template with output tracing
  → variants        one or more (rendered SQL + slice table + provenance)
  → lex → parse → lint           (per variant)
  → map             each diagnostic through the slice table → source positions
  → merge           dedupe across variants, union provenance
```

## 3. Source slices (pre-scan)

Before any execution, a small scanner splits the file on Jinja delimiters
(`{{ }}`, `{% %}`, `{# #}`). It needs no engine and always succeeds, which
matters: even an unrenderable template yields a usable slice list for the
fallback path (§8). It produces the **source-side** slice list:

```go
type SliceKind int

const (
    Literal    SliceKind = iota // plain SQL
    Templated                   // {{ ... }}
    BlockStart                  // {% if %}, {% for %}, {% macro %}
    BlockMid                    // {% else %}, {% elif %}
    BlockEnd                    // {% endif %}, {% endfor %}
    Comment                     // {# ... #}
)

type Slice struct {
    SourceSpan   source.Range // in the original file (byte offsets)
    RenderedSpan source.Range // in a rendered variant; zero until rendered
    Kind         SliceKind
}
```

## 4. Rendering with output tracing

A Go Jinja engine will not hand you byte positions for free, so the renderer
**instruments** the template with **boundary markers**: a unique sentinel is
inserted immediately after each Jinja tag, then located in the output.

```
instrument("SELECT {{ ref('a') }} FROM x")
      →  "SELECT {{ ref('a') }}§1§ FROM x"

rendered  →  "SELECT orders§1§ FROM x"

locate §1§ ⇒
    "SELECT "  ↔ literal before the tag
    "orders"   ↔ source range of {{ ref('a') }}   (Templated)
    " FROM x"  ↔ literal after the tag
```

Every rendered byte now belongs to exactly one slice, so mapping is a binary
search plus arithmetic (§5). Sentinels survive arbitrary control flow:

- A marker only appears if its branch actually ran, so untaken arms contribute
  no output and raise no false positives.
- A loop body's marker appears **once per iteration**, and every copy maps back
  to the same loop-body source span — exactly what linting wants:

```
{% for c in cols %}{{ c }}{% endfor %}   →   a§1§b§1§c§1§
```

Sentinels use a collision-proof token (a NUL-delimited marker such as
`\x00RUSQ<n>\x00`) so they can never clash with real SQL content. **[decision]**

An alternative is to hook the engine's writer (`Template.ExecuteWriter` with a
custom `io.Writer`) plus an AST walk to recover positions directly. Boundary
markers are simpler, engine-agnostic, and robust across control flow, so they
are the starting point. **[decision]**

## 5. Source mapping

Mapping a rendered byte offset back to source:

- **Literal** slice → `source = renderedOffset − renderedStart + sourceStart`
  (exact line and column; the rendered text is byte-identical to the source
  text).
- **Templated** / **Block** slices → report at `sourceStart` (the start of the
  tag), because a rendered value has no 1:1 correspondence to the tag that
  produced it.

A fix that edits only literal slices is applied directly to the source at the
mapped offset. A fix that would touch a templated slice is **downgraded to
display-only** (no auto-apply) and explains why. **[decision]**

## 6. Variant enumeration

One render shows only the branches that were taken. To lint the rest:

1. Register a Jinja `Undefined` that **records** every undefined variable
   instead of raising.
2. After the primary render, for each recorded symbol — and for each untaken
   `{% if %}` / `{% else %}` arm — re-render with that symbol stubbed to a
   representative value (`0`, `''`, `'x'`, or a configured sample). Each
   re-render is a **variant**.
3. For a `{% for %}` over an undefined iterable, render once with a
   one-element mock list.
4. Stop at `render-variant-limit` (default 5, SPEC §9).

A finding that appears in several variants is reported **once**, with
provenance listing them:

```go
type Variant struct {
    Rendered   []byte     // real SQL after rendering
    Slices     []Slice    // rendered ⇄ source map
    Provenance []string   // "primary", "var:target.name=prod", "else:is_incremental"
}

type Templater interface {
    Render(src source.Text, s Settings) ([]Variant, []SyntaxError)
}
```

Determinism matters: variants are enumerated in a stable order and findings are
merged deterministically, so the same file always produces the same report
(SPEC §12.7). **[decision]**

## 7. dbt / SQLMesh builtins

The dbt surface is implemented as Jinja globals so dbt-flavoured templates
render **without** dbt installed (the `apply-dbt-builtins` flag in SPEC §9):

| Template | Resolution |
|---|---|
| `ref('orders')` | relation stub `orders`, or mapped via the cross-file index (SPEC §8.3) |
| `source('raw', 'orders')` | `raw_orders` |
| `var('x', default)` | supplied context value, else the default |
| `config(...)`, `this`, `target`, `is_incremental()`, `adapter.*` | recorded / registered stubs |

Macros come from `load-macros-from-path` (minus `exclude-macros-from-path`);
`[templaters.jinja.context]` supplies variables. `loader-search-path` and
`library-path` control includes and extension modules. **[decision]**

## 8. Failure handling and substitution mode

Templating must **never abort a run**. When a template cannot be executed —
syntax error, missing include, unknown macro — rusq emits a templater diagnostic
and falls back to **substitution mode** rather than failing the file:

- Each `{{ … }}` is replaced by a placeholder token that **preserves newlines
  and columns**, so the surrounding literal SQL still lints at correct
  positions.
- `{% if %}` / `{% else %}` **arms are treated as concatenated**, so the SQL in
  every branch is visible to the rules. This is the non-executing path.

Substitution mode is also directly selectable via `ignore-templating`. It needs
no engine and never crashes, which makes it a safe default while the executing
path matures (§12). **[decision]**

## 9. Engine choice

The `jinja` templater uses **`pongo2`** (`flosch/pongo2`) — mature, maintained,
Jinja/Django syntax, with `{% if %}`, `{% for %}`, macros, inheritance, filters,
custom tags/filters/globals, and custom writers. (`gonja` is a more literal
Jinja2 port but is less maintained.) The `Templater` interface (§6) isolates the
choice so it can be swapped without touching the rest of the pipeline.
**[decision]**

## 10. Templater interface

All templaters implement one interface (SPEC §12.3). The concrete set:

| Templater | Mode | Engine |
|---|---|---|
| `raw` | pass-through | none |
| `placeholder` | lexical | regex per param style |
| `jinja` | render + substitution | `pongo2` |
| `python` | lexical (best-effort) | f-string/`.format()` scanner |
| `dbt` / `sqlmesh` | external (optional) | Python install, else degrade to `jinja` |

## 11. Rule interaction

- `JJ01` (`jinja.padding`) is the one rule that runs on the **raw source**,
  checking for single spaces inside `{{ }}` / `{% %}`. It activates only when
  the jinja templater is in use and is independent of rendering.
- Every other rule runs on each rendered variant and reports through the slice
  map (§5).
- The **formatter never rewrites template tags**; it formats only literal
  slices (SPEC §10). Formatting is idempotent with respect to the template.

## 12. Phasing

| Phase | Delivers | Notes |
|---|---|---|
| 1 | pre-scan + substitution mode | no engine; all branches visible; ships early |
| 2 | executing `pongo2` renderer: sentinel tracing, variant enumeration, dbt builtins | the fidelity core |
| 3 | optional full-fidelity `dbt`/`sqlmesh` bridges | external Python; degrade gracefully |

## 13. Open questions

- **[open]** Whether `pongo2`'s writer can be hooked to recover positions
  without marker injection, avoiding sentinel bookkeeping for the common case.
- **[open]** Exact sample values used when stubbing undefined symbols, and
  whether they should be user-configurable.
- **[open]** How aggressively to enumerate `if`/`else` chains before hitting
  `render-variant-limit` (breadth of branches vs. render cost).