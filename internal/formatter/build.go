package formatter

import (
	"strings"

	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/settings"
	"github.com/ppreeper/rusq/internal/tree"
)

// builder converts a lossless CST into the document IR. It never drops or
// reorders significant tokens: only trivia (whitespace/newlines) is replaced by
// computed layout, which is what makes formatting safe and idempotent.
type builder struct {
	s settings.Settings
}

func newBuilder(s settings.Settings) *builder { return &builder{s: s} }

// chunkClass describes how a clause chunk is laid out.
type chunkClass uint8

const (
	clsNone   chunkClass = iota // not a clause start; plain expression
	clsBlock                    // keyword on its own line when the body does not fit
	clsInline                   // keyword and body always on one line
	clsJoin                     // join clause: ON/USING may drop to its own line
)

// chunk pairs a rendered clause with its class so the join loop can decide how
// to separate it from the previous clause (keyword-line-position).
type chunk struct {
	d   *doc
	cls chunkClass
}

// keywordTrailing reports whether clause keywords end the preceding line rather
// than opening a new one.
func (b *builder) keywordTrailing() bool {
	p := b.s.Format.TypeLinePosition("keyword", b.s.Format.KeywordLinePosition)
	return strings.EqualFold(p, "trailing")
}

// commaLeading reports whether list commas lead the line. A per-type override
// for "comma" wins; comma-style is the specific knob; line-position is the
// fallback for the comma/operator families.
func (b *builder) commaLeading() bool {
	style := b.s.Format.CommaStyle
	if style == "" {
		style = b.s.Format.LinePosition
	}
	p := b.s.Format.TypeLinePosition("comma", style)
	return strings.EqualFold(p, "leading")
}

// operatorTrailing reports whether binary/boolean operators end the line.
func (b *builder) operatorTrailing() bool {
	p := b.s.Format.TypeLinePosition("operator", b.s.Format.LinePosition)
	return strings.EqualFold(p, "trailing")
}

// singleTargetNewLine reports whether a one-target SELECT puts its target on a
// new line.
func (b *builder) singleTargetNewLine() bool {
	p := strings.ReplaceAll(b.s.Format.SingleTargetPolicy, "-", "_")
	return strings.EqualFold(p, "new_line")
}

// clause keyword tables (lower-case).
var (
	blockHeads = map[string]bool{
		"select": true, "from": true, "where": true, "having": true,
		"limit": true, "offset": true, "values": true, "returning": true,
		"union": true, "intersect": true, "except": true, "minus": true,
		"set": true,
	}
	inlineHeads = map[string]bool{
		"with": true, "insert": true, "update": true, "delete": true,
		"create": true, "alter": true, "drop": true, "merge": true,
		"truncate": true, "replace": true, "use": true,
		"begin": true, "commit": true, "rollback": true, "grant": true,
		"revoke": true, "comment": true, "call": true, "explain": true,
		"analyze": true, "vacuum": true, "copy": true, "declare": true,
		"fetch": true, "close": true, "prepare": true, "execute": true,
		"deallocate": true, "join": true,
	}
	// nameIntroducers are keywords after which an identifier is a name, not a
	// function, so a following parenthesis takes a space: CREATE TABLE x (…).
	nameIntroducers = map[string]bool{
		"table": true, "into": true, "exists": true, "index": true,
		"view": true, "trigger": true, "schema": true, "database": true,
		"sequence": true, "temporary": true, "temp": true, "unlogged": true,
		"materialized": true, "only": true,
	}
	joinMods = map[string]bool{
		"inner": true, "left": true, "right": true, "full": true,
		"cross": true, "outer": true, "natural": true, "straight_join": true,
	}
	// nonFunctionKeywords are keywords that are followed by a space before a
	// parenthesis, e.g. IN (...), OVER (...), AS (...).
	nonFunctionKeywords = map[string]bool{
		"in": true, "on": true, "using": true, "and": true, "or": true,
		"not": true, "exists": true, "where": true, "select": true,
		"from": true, "as": true, "join": true, "union": true, "all": true,
		"any": true, "some": true, "between": true, "like": true, "ilike": true,
		"case": true, "when": true, "then": true, "else": true, "end": true,
		"returning": true, "set": true, "into": true, "over": true, "with": true,
		"by": true, "distinct": true, "top": true, "limit": true, "offset": true,
		"having": true, "group": true, "order": true, "values": true, "table": true,
		"index": true, "view": true, "primary": true, "foreign": true,
		"references": true, "check": true, "unique": true, "default": true,
		"constraint": true, "partition": true, "filter": true, "within": true,
		"key": true,
	}
)

// buildTree formats every statement in the tree.
func (b *builder) buildTree(t *tree.Tree) *doc {
	if t == nil || t.Root == nil {
		return nil
	}
	var parts []*doc
	for _, n := range t.Root.Children {
		if n.IsLeaf() {
			continue
		}
		d := b.buildStatement(n)
		if d == nil {
			continue
		}
		if len(parts) > 0 {
			// A comment-only node (e.g. a trailing "-- note" after the final
			// terminator) hugs the previous statement instead of starting a
			// new block.
			if isCommentOnly(n) {
				parts = append(parts, hardline())
			} else {
				parts = append(parts, b.statementSeparator())
			}
		}
		parts = append(parts, d)
	}
	if len(parts) == 0 {
		return nil
	}
	return concat(parts...)
}

func (b *builder) buildStatement(n *tree.Node) *doc {
	toks := allTokens(n)
	if len(toks) == 0 {
		return nil
	}
	return b.buildTokens(toks)
}

// buildTokens splits a token slice into clause chunks and joins them with hard
// line breaks. Paren depth is tracked so that keywords inside subqueries or
// function calls do not start new clauses at this level.
func (b *builder) buildTokens(toks []*lexer.Token) *doc {
	if len(toks) == 0 {
		return nil
	}
	depth := parenDepth(toks)

	var chunks []chunk
	start := 0
	headLen, cls := classify(toks, 0)
	addChunk := func(d *doc, cls chunkClass) {
		if d == nil {
			return
		}
		chunks = append(chunks, chunk{d: d, cls: cls})
	}

	i := 1
	for i < len(toks) {
		if depth[i] == 0 && toks[i].Kind != lexer.Comment {
			h, c := classify(toks, i)
			if c != clsNone {
				addChunk(b.buildChunk(toks[start:i], headLen, cls, len(chunks) == 0), cls)
				start, headLen, cls = i, h, c
				i += h
				continue
			}
		}
		i++
	}
	addChunk(b.buildChunk(toks[start:], headLen, cls, len(chunks) == 0), cls)

	var parts []*doc
	var prev *doc
	for i, c := range chunks {
		if i > 0 && !endsWithHardline(prev) {
			if b.keywordTrailing() && c.cls != clsNone {
				parts = append(parts, text(" "))
			} else {
				parts = append(parts, hardline())
			}
		}
		parts = append(parts, c.d)
		prev = c.d
	}
	return concat(parts...)
}

// endsWithHardline reports whether the last leaf of d is a hard line break,
// which is used to avoid emitting a double blank line after a line comment.
func endsWithHardline(d *doc) bool {
	for d != nil && len(d.children) > 0 {
		d = d.children[len(d.children)-1]
	}
	return d != nil && (d.kind == kHardLine || d.kind == kLiteralLine)
}

func (b *builder) buildChunk(toks []*lexer.Token, headLen int, cls chunkClass, first bool) *doc {
	if len(toks) == 0 {
		return nil
	}
	if cls == clsNone {
		return b.bodyDoc(toks)
	}
	if headLen > len(toks) {
		headLen = len(toks)
	}
	head := headText(toks[:headLen])
	body := toks[headLen:]

	switch cls {
	case clsInline:
		if len(body) == 0 {
			return text(head)
		}
		return concat(text(head), text(" "), b.exprDoc(body))
	case clsJoin:
		firstBody, rest := splitTopKeyword(body, "on", "using")
		if rest == nil {
			if len(body) == 0 {
				return text(head)
			}
			return concat(text(head), text(" "), b.exprDoc(body))
		}
		if b.s.Format.AlignFollowing {
			return group(concat(
				text(head), text(" "), b.exprDoc(firstBody),
				align(concat(line(), b.exprDoc(rest))),
			))
		}
		if b.keywordTrailing() && !first {
			return concat(
				text(head), text(" "), b.exprDoc(firstBody),
				indent(concat(hardline(), b.exprDoc(rest))),
			)
		}
		return group(concat(
			text(head), text(" "), b.exprDoc(firstBody),
			indent(concat(line(), b.exprDoc(rest))),
		))
	default: // clsBlock
		if len(body) == 0 {
			return text(head)
		}
		bodyD := b.bodyDoc(body)
		if b.s.Format.AlignFollowing {
			return group(concat(text(head), align(concat(line(), bodyD))))
		}
		// A clause keyword in trailing position closes the previous line, so its
		// body always starts on a new line.
		if b.keywordTrailing() && !first {
			return concat(text(head), indent(concat(hardline(), bodyD)))
		}
		if b.singleTargetNewLine() && isSelectHead(toks, headLen) && countItems(body) == 1 {
			return concat(text(head), indent(concat(hardline(), bodyD)))
		}
		return group(concat(text(head), indent(concat(line(), bodyD))))
	}
}

// statementSeparator builds the gap between two statements: one newline plus
// max-blank-lines empty lines.
func (b *builder) statementSeparator() *doc {
	n := b.s.Format.MaxBlankLines
	if n < 0 {
		n = 0
	}
	parts := make([]*doc, 0, n+1)
	for i := 0; i <= n; i++ {
		parts = append(parts, hardline())
	}
	return concat(parts...)
}

// isSelectHead reports whether this chunk's head is a SELECT (possibly with a
// DISTINCT/ALL modifier).
func isSelectHead(toks []*lexer.Token, headLen int) bool {
	return len(toks) > 0 && headLen > 0 && strings.EqualFold(toks[0].Text, "select")
}

// countItems returns the number of non-empty top-level comma items.
func countItems(toks []*lexer.Token) int {
	n := 0
	for _, it := range splitTopComma(toks) {
		if len(it) > 0 {
			n++
		}
	}
	return n
}

// bodyDoc lays out a clause body: comma-separated items, each of which may break
// at top-level boolean operators.
func (b *builder) bodyDoc(toks []*lexer.Token) *doc {
	items := splitTopComma(toks)
	var parts []*doc
	for _, it := range items {
		if len(it) == 0 {
			continue
		}
		if len(parts) > 0 {
			parts = append(parts, b.commaSeparator())
		}
		parts = append(parts, b.boolDoc(it))
	}
	return concat(parts...)
}

// commaSeparator returns the doc between two list items, honouring comma-style.
// Trailing: "a,\nb". Leading: "a\n, b".
func (b *builder) commaSeparator() *doc {
	if b.commaLeading() {
		return concat(ifBreak(concat(line(), text(",")), text(",")), text(" "))
	}
	return concat(text(","), line())
}

func (b *builder) boolDoc(toks []*lexer.Token) *doc {
	segs := splitTopBool(toks)
	if len(segs) <= 1 {
		return b.exprDoc(toks)
	}
	if b.operatorTrailing() {
		// Trailing operators: "a AND\nb".
		parts := []*doc{b.exprDoc(segs[0])}
		for _, s := range segs[1:] {
			if len(s) == 0 {
				continue
			}
			parts = append(parts, text(" "), text(s[0].Text), line(), b.exprDoc(s[1:]))
		}
		return concat(parts...)
	}
	var parts []*doc
	for i, s := range segs {
		if i > 0 {
			parts = append(parts, line())
		}
		parts = append(parts, b.exprDoc(s))
	}
	return concat(parts...)
}

// exprDoc lays out a token sequence with normalised spacing, recursing into
// parentheses.
func (b *builder) exprDoc(toks []*lexer.Token) *doc {
	var parts []*doc
	var prev, prevPrev *lexer.Token

	for i := 0; i < len(toks); i++ {
		t := toks[i]

		if t.Kind == lexer.Comment {
			if prev != nil {
				parts = append(parts, text(" "))
			}
			parts = append(parts, text(t.Text))
			if isLineComment(t) {
				if prev != nil {
					// A trailing comment must not drag the following clause
					// into this block's indentation: break at the outer level.
					parts = append(parts, dedent(hardline()))
				} else {
					parts = append(parts, hardline())
				}
				prev, prevPrev = nil, nil
				continue
			}
			prevPrev, prev = prev, t
			continue
		}

		if t.Kind == lexer.LParen {
			if j := matchParen(toks, i); j > i && toks[j].Kind == lexer.RParen {
				inner := toks[i+1 : j]
				if prev != nil && needsSpace(prev, t, prevPrev) {
					parts = append(parts, text(" "))
				}
				if innerDoc := b.buildTokens(inner); innerDoc != nil {
					// cte-bracket-newline puts the closing bracket of a CTE
					// body on its own line, forcing the group to break.
					if b.s.Format.CteBracketNewline && isCTEBody(prev, inner) {
						parts = append(parts, concat(
							text("("),
							indent(concat(hardline(), innerDoc)),
							hardline(),
							text(")"),
						))
					} else {
						parts = append(parts, group(concat(
							text("("),
							indent(concat(softline(), innerDoc)),
							softline(),
							text(")"),
						)))
					}
				} else {
					parts = append(parts, text("("), text(")"))
				}
				prevPrev, prev = prev, toks[j]
				i = j
				continue
			}
		}

		if prev != nil && needsSpace(prev, t, prevPrev) {
			parts = append(parts, text(" "))
		}
		parts = append(parts, text(t.Text))
		prevPrev, prev = prev, t
	}
	return concat(parts...)
}

// isCommentOnly reports whether n holds only comments and trivia.
func isCommentOnly(n *tree.Node) bool {
	sawComment := false
	for _, t := range n.Leaves(nil) {
		switch t.Kind {
		case lexer.Comment:
			sawComment = true
		case lexer.Whitespace, lexer.Newline, lexer.EOF:
		default:
			return false
		}
	}
	return sawComment
}

// allTokens returns the leaves of n excluding whitespace and newlines but
// keeping comments.
func allTokens(n *tree.Node) []*lexer.Token {
	leaves := n.Leaves(nil)
	out := make([]*lexer.Token, 0, len(leaves))
	for _, t := range leaves {
		if t.Kind == lexer.Whitespace || t.Kind == lexer.Newline || t.Kind == lexer.EOF {
			continue
		}
		out = append(out, t)
	}
	return out
}

func headText(toks []*lexer.Token) string {
	var sb strings.Builder
	for i, t := range toks {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
	}
	return sb.String()
}

func parenDepth(toks []*lexer.Token) []int {
	out := make([]int, len(toks))
	d := 0
	for i, t := range toks {
		out[i] = d
		switch t.Kind {
		case lexer.LParen:
			d++
		case lexer.RParen:
			if d > 0 {
				d--
			}
		}
	}
	return out
}

// classify reports whether the token at i starts a clause chunk, and how many
// tokens make up its head.
func classify(toks []*lexer.Token, i int) (int, chunkClass) {
	if i >= len(toks) || toks[i].Kind == lexer.Comment {
		return 0, clsNone
	}
	low := strings.ToLower(toks[i].Text)
	switch {
	case low == "from" && prevWord(toks, i) == "delete", low == "from" && prevWord(toks, i) == "truncate":
		return 0, clsNone
	case low == "select":
		n := 1
		if i+n < len(toks) {
			switch strings.ToLower(toks[i+n].Text) {
			case "distinct", "all":
				n++
			}
		}
		return n, clsBlock
	case low == "group" || low == "order":
		if i+1 < len(toks) && strings.EqualFold(toks[i+1].Text, "by") {
			return 2, clsBlock
		}
		return 0, clsNone
	case low == "join":
		return 1, clsJoin
	case joinMods[low]:
		n := 1
		for i+n < len(toks) && n < 3 {
			l := strings.ToLower(toks[i+n].Text)
			if l == "outer" {
				n++
				continue
			}
			if l == "join" {
				return n + 1, clsJoin
			}
			break
		}
		return 0, clsNone
	case low == "union" || low == "intersect" || low == "except" || low == "minus":
		n := 1
		if i+1 < len(toks) {
			switch strings.ToLower(toks[i+1].Text) {
			case "all", "distinct":
				n++
			}
		}
		return n, clsBlock
	case blockHeads[low]:
		return 1, clsBlock
	case inlineHeads[low]:
		return 1, clsInline
	}
	return 0, clsNone
}

// prevWord returns the lower-case text of the previous non-comment token.
func prevWord(toks []*lexer.Token, i int) string {
	for j := i - 1; j >= 0; j-- {
		if toks[j].Kind == lexer.Comment {
			continue
		}
		return strings.ToLower(toks[j].Text)
	}
	return ""
}

// splitTopComma splits on commas that are not nested in parentheses.
func splitTopComma(toks []*lexer.Token) [][]*lexer.Token {
	var out [][]*lexer.Token
	start, d := 0, 0
	for i, t := range toks {
		switch t.Kind {
		case lexer.LParen:
			d++
		case lexer.RParen:
			if d > 0 {
				d--
			}
		case lexer.Comma:
			if d == 0 {
				out = append(out, toks[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, toks[start:])
	return out
}

// splitTopBool splits before top-level AND/OR so the operator leads the line.
func splitTopBool(toks []*lexer.Token) [][]*lexer.Token {
	var out [][]*lexer.Token
	start, d := 0, 0
	for i, t := range toks {
		switch t.Kind {
		case lexer.LParen:
			d++
		case lexer.RParen:
			if d > 0 {
				d--
			}
		}
		if d != 0 || t.Kind != lexer.Keyword {
			continue
		}
		l := strings.ToLower(t.Text)
		if (l == "and" || l == "or") && i > start {
			out = append(out, toks[start:i])
			start = i
		}
	}
	out = append(out, toks[start:])
	return out
}

// splitTopKeyword splits before the first top-level occurrence of one of kws.
func splitTopKeyword(toks []*lexer.Token, kws ...string) ([]*lexer.Token, []*lexer.Token) {
	d := 0
	for i, t := range toks {
		switch t.Kind {
		case lexer.LParen:
			d++
		case lexer.RParen:
			if d > 0 {
				d--
			}
		}
		if d != 0 || t.Kind != lexer.Keyword {
			continue
		}
		for _, kw := range kws {
			if strings.EqualFold(t.Text, kw) {
				return toks[:i], toks[i:]
			}
		}
	}
	return toks, nil
}

func matchParen(toks []*lexer.Token, i int) int {
	d := 0
	for j := i; j < len(toks); j++ {
		switch toks[j].Kind {
		case lexer.LParen:
			d++
		case lexer.RParen:
			d--
			if d == 0 {
				return j
			}
		}
	}
	return -1
}

func isLineComment(t *lexer.Token) bool {
	return strings.HasPrefix(t.Text, "--") || strings.HasPrefix(t.Text, "#")
}

// isCTEBody reports whether the parenthesised sequence looks like a common
// table expression body: an AS keyword followed by a SELECT or nested WITH.
func isCTEBody(prev *lexer.Token, inner []*lexer.Token) bool {
	if prev == nil || !strings.EqualFold(prev.Text, "AS") || len(inner) == 0 {
		return false
	}
	switch strings.ToUpper(inner[0].Text) {
	case "SELECT", "WITH":
		return true
	}
	return false
}

// needsSpace reports whether a space belongs between prev and cur.
func needsSpace(p, c, pp *lexer.Token) bool {
	if p == nil || c == nil {
		return false
	}
	switch c.Kind {
	case lexer.Comma, lexer.Semicolon, lexer.RParen, lexer.RBracket, lexer.Dot:
		return false
	}
	if c.Kind == lexer.Operator && c.Text == "::" {
		return false
	}
	switch p.Kind {
	case lexer.LParen, lexer.LBracket, lexer.Dot:
		return false
	}
	if p.Kind == lexer.Operator && p.Text == "::" {
		return false
	}
	if c.Kind == lexer.LParen {
		if p.Kind == lexer.Identifier && pp != nil && pp.Kind == lexer.Keyword &&
			nameIntroducers[strings.ToLower(pp.Text)] {
			return true
		}
		switch p.Kind {
		case lexer.Identifier, lexer.QuotedIdentifier, lexer.RParen:
			return false
		}
		if p.Kind == lexer.Keyword && !nonFunctionKeywords[strings.ToLower(p.Text)] {
			return false
		}
		return true
	}
	if p.Kind == lexer.Operator && (p.Text == "-" || p.Text == "+") {
		if isUnaryAfter(pp) {
			return false
		}
	}
	return true
}

func isUnaryAfter(pp *lexer.Token) bool {
	if pp == nil {
		return true
	}
	switch pp.Kind {
	case lexer.Comma, lexer.LParen, lexer.Operator, lexer.Keyword, lexer.Semicolon:
		return true
	}
	return false
}
