package cli

import (
	"strings"
	"testing"
)

func TestRenderUnitDiffMixedChange(t *testing.T) {
	out := renderUnitDiff("acron-x.service", "a\nb\nc\n", "a\nB\nc\n")

	want := "--- a/acron-x.service\n" +
		"+++ b/acron-x.service\n" +
		"@@ -1,3 +1,3 @@\n" +
		" a\n" +
		"-b\n" +
		"+B\n" +
		" c\n"
	if out != want {
		t.Errorf("renderUnitDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitFullShowsEveryLineWithoutHunkHeader(t *testing.T) {
	installed := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\n"
	desired := "l1\nl2\nl3\nl4\nL5\nl6\nl7\nl8\n"
	out := renderUnitFull("u", installed, desired)

	want := "--- a/u\n" +
		"+++ b/u\n" +
		" l1\n l2\n l3\n l4\n" +
		"-l5\n" +
		"+L5\n" +
		" l6\n l7\n l8\n"
	if out != want {
		t.Errorf("renderUnitFull =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDiffCreateAgainstDevNull(t *testing.T) {
	out := renderUnitDiff("acron-x.timer", "", "[Timer]\nOnCalendar=*-*-* 02:00:00\n")

	for _, want := range []string{
		"--- /dev/null\n",
		"+++ b/acron-x.timer\n",
		"@@ -0,0 +1,2 @@\n",
		"+[Timer]\n",
		"+OnCalendar=*-*-* 02:00:00\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("create diff missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "--- a/") {
		t.Errorf("create diff should anchor the old side at /dev/null\n---\n%s", out)
	}
}

func TestRenderUnitDiffRemoveAgainstDevNull(t *testing.T) {
	out := renderUnitDiff("acron-x.service", "[Service]\nType=oneshot\n", "")

	for _, want := range []string{
		"--- a/acron-x.service\n",
		"+++ /dev/null\n",
		"@@ -1,2 +0,0 @@\n",
		"-[Service]\n",
		"-Type=oneshot\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("remove diff missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderUnitDiffThreeLinesOfContext(t *testing.T) {
	lines := func(mid string) string {
		return "l1\nl2\nl3\nl4\n" + mid + "\nl6\nl7\nl8\nl9\nl10\n"
	}
	out := renderUnitDiff("u", lines("l5"), lines("L5"))

	if !strings.Contains(out, "@@ -2,7 +2,7 @@\n") {
		t.Errorf("want hunk header anchored 3 lines before the change\n---\n%s", out)
	}
	body := " l2\n l3\n l4\n-l5\n+L5\n l6\n l7\n l8\n"
	if !strings.Contains(out, body) {
		t.Errorf("want exactly 3 context lines each side\n---\n%s", out)
	}
	for _, beyond := range []string{"l1", "l9", "l10"} {
		if strings.Contains(out, beyond) {
			t.Errorf("context should not reach %q\n---\n%s", beyond, out)
		}
	}
}

func TestRenderUnitDiffSplitsDistantChangesIntoHunks(t *testing.T) {
	// Seven unchanged lines between the two edits exceed 2*3 context, so the
	// changes land in separate hunks.
	body := func(first, last string) string {
		return first + "\nc1\nc2\nc3\nc4\nc5\nc6\nc7\n" + last + "\n"
	}
	out := renderUnitDiff("u", body("x", "y"), body("X", "Y"))

	if got := strings.Count(out, "@@ "); got != 2 {
		t.Errorf("want two hunks, got %d\n---\n%s", got, out)
	}
	for _, want := range []string{"@@ -1,4 +1,4 @@\n", "@@ -6,4 +6,4 @@\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing hunk header %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, " c4\n") {
		t.Errorf("the middle unchanged line should fall outside both hunks\n---\n%s", out)
	}
}

func TestRenderUnitDiffMarksMissingFinalNewline(t *testing.T) {
	// Desired drops the final newline the installed side has; git marks the new
	// side with "\ No newline at end of file" after the changed line.
	out := renderUnitDiff("acron-x.service", "only\n", "only")

	want := "--- a/acron-x.service\n" +
		"+++ b/acron-x.service\n" +
		"@@ -1 +1 @@\n" +
		"-only\n" +
		"+only\n" +
		"\\ No newline at end of file\n"
	if out != want {
		t.Errorf("renderUnitDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDiffMarksGainedFinalNewline(t *testing.T) {
	// Installed lacks the final newline desired adds; the marker follows the old
	// side's line, before the added line.
	out := renderUnitDiff("u", "only", "only\n")

	want := "--- a/u\n" +
		"+++ b/u\n" +
		"@@ -1 +1 @@\n" +
		"-only\n" +
		"\\ No newline at end of file\n" +
		"+only\n"
	if out != want {
		t.Errorf("renderUnitDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDiffSurfacesExtraTrailingNewline(t *testing.T) {
	// Installed has an extra trailing newline (a blank last line); both sides
	// stay newline-terminated, so the delta is a deleted empty line, no marker.
	out := renderUnitDiff("u", "x\n\n", "x\n")

	want := "--- a/u\n" +
		"+++ b/u\n" +
		"@@ -1,2 +1 @@\n" +
		" x\n" +
		"-\n"
	if out != want {
		t.Errorf("renderUnitDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitFullMarksMissingFinalNewline(t *testing.T) {
	out := renderUnitFull("u", "a\nb\n", "a\nb")

	want := "--- a/u\n" +
		"+++ b/u\n" +
		" a\n" +
		"-b\n" +
		"+b\n" +
		"\\ No newline at end of file\n"
	if out != want {
		t.Errorf("renderUnitFull =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDiffMarksUnchangedLastLineWithoutNewline(t *testing.T) {
	// The final line is unchanged but unterminated on both sides; git marks the
	// context line itself, so the marker rides an equal op, not only -/+ ops.
	out := renderUnitDiff("u", "a\nctx", "A\nctx")

	want := "--- a/u\n" +
		"+++ b/u\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-a\n" +
		"+A\n" +
		" ctx\n" +
		"\\ No newline at end of file\n"
	if out != want {
		t.Errorf("renderUnitDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDiffMarksBothSidesMissingNewline(t *testing.T) {
	// Both sides' final line is unterminated and differs; git emits a marker
	// after each side's line independently.
	out := renderUnitDiff("u", "old", "new")

	want := "--- a/u\n" +
		"+++ b/u\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"\\ No newline at end of file\n" +
		"+new\n" +
		"\\ No newline at end of file\n"
	if out != want {
		t.Errorf("renderUnitDiff =\n%q\nwant\n%q", out, want)
	}
}

func TestRenderUnitDiffOmitsCountForSingleLineRange(t *testing.T) {
	// A one-line old side renders "@@ -1 +1,2 @@", matching git's omission of a
	// unit count.
	out := renderUnitDiff("u", "only\n", "only\nadded\n")

	if !strings.Contains(out, "@@ -1 +1,2 @@\n") {
		t.Errorf("want a count-omitted old range\n---\n%s", out)
	}
}
