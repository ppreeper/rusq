// Package tree defines the lossless concrete syntax tree (CST). Every token,
// including trivia, is a leaf; Unparse walks the leaves in order and therefore
// round-trips the source byte-for-byte.
package tree

import (
	"strings"

	"github.com/ppreeper/rusq/internal/diagnostic"
	"github.com/ppreeper/rusq/internal/lexer"
	"github.com/ppreeper/rusq/internal/source"
)

// Kind names a CST node.
type Kind uint8

const (
	Root Kind = iota
	Statement
	Batch
	Select
	Insert
	Update
	Delete
	Create
	Alter
	Drop
	Merge
	With
	Cte
	SelectClause
	FromClause
	WhereClause
	GroupByClause
	HavingClause
	OrderByClause
	LimitClause
	OffsetClause
	JoinClause
	SetOperation
	ValuesClause
	ReturningClause
	Expression
	List
	Function
	Identifier
	Literal
	Unknown
	Error
	// TokenLeaf marks a leaf node holding exactly one lexer token.
	TokenLeaf
)

var kindNames = map[Kind]string{
	Root: "root", Statement: "statement", Batch: "batch", Select: "select",
	Insert: "insert", Update: "update", Delete: "delete", Create: "create",
	Alter: "alter", Drop: "drop", Merge: "merge", With: "with", Cte: "cte",
	SelectClause: "select_clause", FromClause: "from_clause", WhereClause: "where_clause",
	GroupByClause: "group_by_clause", HavingClause: "having_clause",
	OrderByClause: "order_by_clause", LimitClause: "limit_clause",
	OffsetClause: "offset_clause", JoinClause: "join_clause",
	SetOperation: "set_operation", ValuesClause: "values_clause",
	ReturningClause: "returning_clause", Expression: "expression", List: "list",
	Function: "function", Identifier: "identifier", Literal: "literal",
	Unknown: "unknown", Error: "error", TokenLeaf: "token",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return "kind"
}

// Node is a CST node. A node is a leaf when Token is non-nil.
type Node struct {
	Kind     Kind
	Children []*Node
	Token    *lexer.Token

	// parent is maintained by Append for upward navigation.
	parent *Node
}

// Leaf returns a leaf node for tok.
func Leaf(tok *lexer.Token) *Node { return &Node{Kind: TokenLeaf, Token: tok} }

// New returns an empty node of the given kind.
func New(kind Kind, children ...*Node) *Node {
	n := &Node{Kind: kind}
	n.Append(children...)
	return n
}

// Append adds children and sets their parent.
func (n *Node) Append(children ...*Node) {
	for _, c := range children {
		if c == nil {
			continue
		}
		c.parent = n
		n.Children = append(n.Children, c)
	}
}

// Parent returns the parent node, or nil for the root.
func (n *Node) Parent() *Node { return n.parent }

// IsLeaf reports whether the node holds a token.
func (n *Node) IsLeaf() bool { return n.Token != nil }

// Walk visits n and its descendants depth-first. Returning false from fn stops
// descending into that node.
func (n *Node) Walk(fn func(*Node) bool) {
	if n == nil || !fn(n) {
		return
	}
	for _, c := range n.Children {
		c.Walk(fn)
	}
}

// Leaves appends the leaves of n to dst in source order.
func (n *Node) Leaves(dst []*lexer.Token) []*lexer.Token {
	if n == nil {
		return dst
	}
	if n.Token != nil {
		return append(dst, n.Token)
	}
	for _, c := range n.Children {
		dst = c.Leaves(dst)
	}
	return dst
}

// FirstToken returns the first leaf token, or nil.
func (n *Node) FirstToken() *lexer.Token {
	for n != nil {
		if n.Token != nil {
			return n.Token
		}
		if len(n.Children) == 0 {
			return nil
		}
		n = n.Children[0]
	}
	return nil
}

// LastToken returns the last leaf token, or nil.
func (n *Node) LastToken() *lexer.Token {
	for n != nil {
		if n.Token != nil {
			return n.Token
		}
		if len(n.Children) == 0 {
			return nil
		}
		n = n.Children[len(n.Children)-1]
	}
	return nil
}

// Span returns the byte range covered by the node's significant tokens. When
// the node has no significant tokens (e.g. a trivia-only segment) it falls back
// to the first and last leaf.
func (n *Node) Span() source.Range {
	leaves := n.Leaves(nil)
	var first, last *lexer.Token
	for _, t := range leaves {
		if !t.IsSignificant() {
			continue
		}
		if first == nil {
			first = t
		}
		last = t
	}
	if first == nil {
		if len(leaves) == 0 {
			return source.Range{}
		}
		first = leaves[0]
		last = leaves[len(leaves)-1]
	}
	return source.Range{Start: first.Span.Start, End: last.Span.End}
}

// Text reassembles the node's leaves.
func (n *Node) Text() string {
	var b strings.Builder
	for _, t := range n.Leaves(nil) {
		if t.Kind != lexer.EOF {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// ChildrenOf returns the direct children of n with the given kind.
func (n *Node) ChildrenOf(kind Kind) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// FirstChild returns the first direct child of the given kind, or nil.
func (n *Node) FirstChild(kind Kind) *Node {
	for _, c := range n.Children {
		if c.Kind == kind {
			return c
		}
	}
	return nil
}

// Tree is a parsed file.
type Tree struct {
	Root    *Node
	Source  []byte
	Dialect string
	Errors  []diagnostic.SyntaxError
}

// Unparse reassembles the source from the tree. It is byte-identical to the
// input for every successfully lexed file.
func Unparse(t *Tree) []byte {
	if t == nil || t.Root == nil {
		return nil
	}
	var b strings.Builder
	if len(t.Source) > 0 {
		b.Grow(len(t.Source))
	}
	for _, tok := range t.Root.Leaves(nil) {
		if tok.Kind == lexer.EOF {
			continue
		}
		b.WriteString(tok.Text)
	}
	return []byte(b.String())
}

// SignificantTokens returns the non-trivia leaves in source order.
func SignificantTokens(n *Node) []*lexer.Token {
	var out []*lexer.Token
	for _, t := range n.Leaves(nil) {
		if t.IsSignificant() {
			out = append(out, t)
		}
	}
	return out
}
