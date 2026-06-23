package cli

import (
	"strings"
	"testing"
)

func TestRenderDiffMarksChangedLines(t *testing.T) {
	before := "a\nb\nc\n"
	after := "a\nB\nc\n"

	out := renderDiff(before, after)

	want := "  a\n" + "- b\n" + "+ B\n" + "  c\n"
	if out != want {
		t.Errorf("renderDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderDiffInsertAndDelete(t *testing.T) {
	out := renderDiff("keep\ndrop\n", "keep\nadd\ntail\n")

	for _, want := range []string{"  keep\n", "- drop\n", "+ add\n", "+ tail\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDiff missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderDiffOneSideEmpty(t *testing.T) {
	if got := renderDiff("", "x\ny\n"); got != "+ x\n+ y\n" {
		t.Errorf("added-only diff = %q", got)
	}
	if got := renderDiff("x\ny\n", ""); got != "- x\n- y\n" {
		t.Errorf("removed-only diff = %q", got)
	}
}
