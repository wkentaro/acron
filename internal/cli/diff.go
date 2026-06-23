package cli

import (
	"fmt"
	"strconv"
	"strings"
)

const diffContext = 3

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

// renderUnitDiff renders the delta from installed to desired as a git-style
// unified diff: a "--- "/"+++ " header (with /dev/null on an absent side), "@@"
// hunks with three lines of context and real line ranges, removed lines in red
// and added lines in green. An absent installed side renders a create (all
// green against /dev/null), an absent desired side a remove (all red), both
// present an in-place update labeled a/<name> and b/<name>.
func renderUnitDiff(name, installed, desired string) string {
	var b strings.Builder
	writeDiffHeader(&b, "--- ", "a/"+name, installed)
	writeDiffHeader(&b, "+++ ", "b/"+name, desired)

	ops := diffLines(splitLines(installed), splitLines(desired))
	for _, h := range groupHunks(ops) {
		writeHunk(&b, ops, h)
	}
	return b.String()
}

// writeDiffHeader writes one "--- "/"+++ " header, anchoring an absent side
// (empty content) at /dev/null as git does for a created or removed file.
func writeDiffHeader(b *strings.Builder, marker, path, content string) {
	if content == "" {
		path = "/dev/null"
	}
	b.WriteString(commentStyle.Render(marker+path) + "\n")
}

type hunk struct{ start, end int } // op indices, half-open

// groupHunks groups changed ops with up to diffContext lines of surrounding
// context. An op is visible when within diffContext ops of a change, so two
// changes separated by no more than 2*diffContext unchanged lines share one hunk
// and a wider gap splits them, matching git's grouping.
func groupHunks(ops []diffOp) []hunk {
	visible := make([]bool, len(ops))
	for k, op := range ops {
		if op.kind == diffEqual {
			continue
		}
		for j := max(k-diffContext, 0); j <= min(k+diffContext, len(ops)-1); j++ {
			visible[j] = true
		}
	}
	var hs []hunk
	for i := 0; i < len(ops); {
		if !visible[i] {
			i++
			continue
		}
		j := i
		for j < len(ops) && visible[j] {
			j++
		}
		hs = append(hs, hunk{i, j})
		i = j
	}
	return hs
}

func writeHunk(b *strings.Builder, ops []diffOp, h hunk) {
	oldStart, newStart := lineNumbers(ops, h.start)
	oldCount, newCount := 0, 0
	var body strings.Builder
	for _, op := range ops[h.start:h.end] {
		switch op.kind {
		case diffEqual:
			oldCount++
			newCount++
			fmt.Fprintf(&body, " %s\n", op.line)
		case diffDelete:
			oldCount++
			body.WriteString(removeStyle.Render("-"+op.line) + "\n")
		case diffInsert:
			newCount++
			body.WriteString(addStyle.Render("+"+op.line) + "\n")
		}
	}
	header := fmt.Sprintf("@@ -%s +%s @@", hunkRange(oldStart, oldCount), hunkRange(newStart, newCount))
	b.WriteString(commentStyle.Render(header) + "\n")
	b.WriteString(body.String())
}

// lineNumbers returns the 1-based old and new line numbers at op index idx.
func lineNumbers(ops []diffOp, idx int) (oldLine, newLine int) {
	oldLine, newLine = 1, 1
	for _, op := range ops[:idx] {
		switch op.kind {
		case diffEqual:
			oldLine++
			newLine++
		case diffDelete:
			oldLine++
		case diffInsert:
			newLine++
		}
	}
	return oldLine, newLine
}

// hunkRange formats one side of an "@@" header. A zero count occurs only for a
// whole-file create or remove (the absent side has no lines); git anchors that
// empty range at line 0, so the start is irrelevant. git also omits the count
// when it is 1.
func hunkRange(start, count int) string {
	if count == 0 {
		return "0,0"
	}
	if count == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
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
