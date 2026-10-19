package output

import (
	"fmt"
	"strings"
)

type opKind byte

const (
	opEqual opKind = ' '
	opDel   opKind = '-'
	opIns   opKind = '+'
)

type diffOp struct {
	kind opKind
	text string
	// line numbers in the old/new files (1-based)
	oldLine int
	newLine int
}

// UnifiedDiff renders a unified diff between a and b. It returns "" when the
// inputs are identical.
func UnifiedDiff(path string, a, b []byte) string {
	if string(a) == string(b) {
		return ""
	}
	al := splitLines(a)
	bl := splitLines(b)
	ops := diffLines(al, bl)

	const context = 3
	// Locate changed operations.
	var changed []int
	for i, op := range ops {
		if op.kind != opEqual {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return ""
	}
	// Group changes whose context windows overlap.
	type group struct{ start, end int }
	var groups []group
	start := maxInt(0, changed[0]-context)
	end := minInt(len(ops), changed[0]+context+1)
	for _, idx := range changed[1:] {
		if idx-context <= end {
			end = minInt(len(ops), idx+context+1)
			continue
		}
		groups = append(groups, group{start, end})
		start = maxInt(0, idx-context)
		end = minInt(len(ops), idx+context+1)
	}
	groups = append(groups, group{start, end})

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n", path)
	fmt.Fprintf(&out, "+++ %s\n", path)
	for _, g := range groups {
		oldStart, newStart := -1, -1
		oldCount, newCount := 0, 0
		for _, op := range ops[g.start:g.end] {
			if op.kind != opIns {
				if oldStart < 0 {
					oldStart = op.oldLine
				}
				oldCount++
			}
			if op.kind != opDel {
				if newStart < 0 {
					newStart = op.newLine
				}
				newCount++
			}
		}
		if oldStart < 0 {
			oldStart = 1
		}
		if newStart < 0 {
			newStart = 1
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, op := range ops[g.start:g.end] {
			out.WriteByte(byte(op.kind))
			out.WriteString(op.text)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func splitLines(b []byte) []string {
	s := string(b)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diffLines computes a line diff with a bounded LCS table.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n*m > 4_000_000 {
		// Fall back to a coarse replace diff for pathological inputs.
		var ops []diffOp
		for i, line := range a {
			ops = append(ops, diffOp{kind: opDel, text: line, oldLine: i + 1, newLine: 1})
		}
		for i, line := range b {
			ops = append(ops, diffOp{kind: opIns, text: line, oldLine: n + 1, newLine: i + 1})
		}
		return ops
	}
	// dp[i][j] = LCS length of a[i:], b[j:].
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			ops = append(ops, diffOp{kind: opEqual, text: a[i], oldLine: i + 1, newLine: j + 1})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, diffOp{kind: opDel, text: a[i], oldLine: i + 1, newLine: j + 1})
			i++
		} else {
			ops = append(ops, diffOp{kind: opIns, text: b[j], oldLine: i + 1, newLine: j + 1})
			j++
		}
	}
	for i < n {
		ops = append(ops, diffOp{kind: opDel, text: a[i], oldLine: i + 1, newLine: j + 1})
		i++
	}
	for j < m {
		ops = append(ops, diffOp{kind: opIns, text: b[j], oldLine: i + 1, newLine: j + 1})
		j++
	}
	return ops
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
