package cli

import (
	"fmt"
	"strconv"
	"strings"
)

const diffContext = 3

// noNewlineMarker is git's annotation for a side whose final line is not
// newline-terminated; it follows that line and is not counted in the @@ ranges.
const noNewlineMarker = `\ No newline at end of file`

type diffKind int

const (
	diffEqual diffKind = iota
	diffDelete
	diffInsert
)

// line is one unit-file line plus whether it was newline-terminated. Tracking
// termination lets the renderers surface a trailing-newline-only drift the way
// git does, instead of normalizing it away before the diff.
type line struct {
	text       string
	hasNewline bool
}

type diffOp struct {
	kind diffKind
	line line
}

// renderUnitDiff renders the delta from installed to desired as a git-style
// unified diff: a "--- "/"+++ " header (with /dev/null on an absent side), "@@"
// hunks with three lines of context and real line ranges, removed lines in red
// and added lines in green. An absent installed side renders a create (all
// green against /dev/null), an absent desired side a remove (all red), both
// present an in-place update labeled a/<name> and b/<name>. `apply --dry-run`
// uses this: the focus is the delta, so unchanged regions collapse to hunks.
func renderUnitDiff(name, installed, desired string) string {
	b, ops := startRender(name, installed, desired)
	for _, h := range groupHunks(ops) {
		oldStart, newStart := lineNumbers(ops, h.start)
		body, oldCount, newCount := hunkBody(ops, h)
		hdr := fmt.Sprintf("@@ -%s +%s @@", hunkRange(oldStart, oldCount), hunkRange(newStart, newCount))
		b.WriteString(commentStyle.Render(hdr) + "\n")
		b.WriteString(body)
	}
	return b.String()
}

// renderUnitFull renders the whole unit with drifted lines marked inline (-/+)
// and no "@@" hunk headers: every line is shown as one span. `show` uses this
// because the unit's full content is the point and drift is a secondary
// annotation, unlike `apply --dry-run` where the delta is the whole focus.
func renderUnitFull(name, installed, desired string) string {
	b, ops := startRender(name, installed, desired)
	writeLines(b, ops, hunk{0, len(ops)})
	return b.String()
}

// startRender writes the "--- "/"+++ " header pair and returns the builder and
// the computed ops so both render functions share the identical prologue.
func startRender(name, installed, desired string) (*strings.Builder, []diffOp) {
	var b strings.Builder
	writeDiffHeader(&b, "--- ", "a/"+name, installed)
	writeDiffHeader(&b, "+++ ", "b/"+name, desired)
	ops := diffLines(splitLines(installed), splitLines(desired))
	return &b, ops
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

// hunkBody renders the lines of a hunk and returns the colored output along
// with the old and new line counts needed for @@ range formatting.
func hunkBody(ops []diffOp, h hunk) (string, int, int) {
	var b strings.Builder
	oldCount, newCount := writeLines(&b, ops, h)
	return b.String(), oldCount, newCount
}

// writeLines writes the colored diff lines of h directly into b and returns
// the old and new line counts. Both renderUnitDiff (via hunkBody) and
// renderUnitFull call this; the latter writes straight into the outer builder
// so no intermediate string allocation is needed.
func writeLines(b *strings.Builder, ops []diffOp, h hunk) (oldCount, newCount int) {
	for _, op := range ops[h.start:h.end] {
		switch op.kind {
		case diffEqual:
			oldCount++
			newCount++
			fmt.Fprintf(b, " %s\n", op.line.text)
		case diffDelete:
			oldCount++
			b.WriteString(removeStyle.Render("-"+op.line.text) + "\n")
		case diffInsert:
			newCount++
			b.WriteString(addStyle.Render("+"+op.line.text) + "\n")
		}
		if !op.line.hasNewline {
			b.WriteString(commentStyle.Render(noNewlineMarker) + "\n")
		}
	}
	return oldCount, newCount
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

// splitLines splits text into lines, tracking newline termination so a
// trailing-newline difference survives into the diff. A final "\n" terminates
// the last line (and is not itself a line); its absence marks the last line as
// unterminated. Lines compare unequal when they differ in either text or
// termination, so git's "\ No newline at end of file" delta is preserved.
func splitLines(text string) []line {
	if text == "" {
		return nil
	}
	hasFinalNewline := strings.HasSuffix(text, "\n")
	parts := strings.Split(text, "\n")
	if hasFinalNewline {
		parts = parts[:len(parts)-1]
	}
	lines := make([]line, len(parts))
	for i, p := range parts {
		lines[i] = line{text: p, hasNewline: i < len(parts)-1 || hasFinalNewline}
	}
	return lines
}

// diffLines is a longest-common-subsequence line diff: shared lines are equal,
// lines only in before are deletions, lines only in after are insertions.
func diffLines(before, after []line) []diffOp {
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

func lcsTable(before, after []line) [][]int {
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
