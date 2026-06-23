package cli

import (
	"fmt"
	"strings"
)

type diffKind int

const (
	diffEqual diffKind = iota
	diffDelete
	diffInsert
)

type diffOp struct {
	kind diffKind
	line string
}

// renderDiff shows a line diff of before against after: shared lines are
// indented, removed lines are prefixed "-", added lines "+".
func renderDiff(before, after string) string {
	var b strings.Builder
	for _, op := range diffLines(splitLines(before), splitLines(after)) {
		switch op.kind {
		case diffEqual:
			fmt.Fprintf(&b, "  %s\n", op.line)
		case diffDelete:
			b.WriteString(removeStyle.Render("- "+op.line) + "\n")
		case diffInsert:
			b.WriteString(addStyle.Render("+ "+op.line) + "\n")
		}
	}
	return b.String()
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// diffLines is a longest-common-subsequence line diff: shared lines are equal,
// lines only in before are deletions, lines only in after are insertions.
func diffLines(before, after []string) []diffOp {
	lcs := lcsTable(before, after)
	var ops []diffOp
	i, j := 0, 0
	for i < len(before) && j < len(after) {
		switch {
		case before[i] == after[j]:
			ops = append(ops, diffOp{diffEqual, before[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{diffDelete, before[i]})
			i++
		default:
			ops = append(ops, diffOp{diffInsert, after[j]})
			j++
		}
	}
	for ; i < len(before); i++ {
		ops = append(ops, diffOp{diffDelete, before[i]})
	}
	for ; j < len(after); j++ {
		ops = append(ops, diffOp{diffInsert, after[j]})
	}
	return ops
}

func lcsTable(before, after []string) [][]int {
	lcs := make([][]int, len(before)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(after)+1)
	}
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			switch {
			case before[i] == after[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	return lcs
}
