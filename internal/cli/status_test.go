package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/scheduler"
)

func TestRenderStatusTable(t *testing.T) {
	tbl := statusTable()
	tbl.Row("job-a", "drifted", "success", "2026-01-01 00:00", "—")
	tbl.Row("job-b", "applied", "never run", "", "2026-01-02 00:00") // never-run jobs have no last-run timestamp

	out := renderStatusTable(tbl)

	if strings.HasSuffix(out, "\n\n") {
		t.Errorf("output ends with a blank line: %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
	for _, header := range []string{"JOB", "APPLY", "STATUS", "LAST", "NEXT"} {
		if !strings.Contains(lines[0], header) {
			t.Errorf("header row %q missing column %q", lines[0], header)
		}
	}
	if len(lines) != 3 {
		t.Errorf("want header + 2 rows = 3 lines, got %d: %q", len(lines), lines)
	}
}

func TestRenderNext(t *testing.T) {
	now := time.Date(2026, 6, 22, 23, 21, 0, 0, time.UTC)
	placeholder := commentStyle.Render("—")
	applied := config.Job{Schedule: "*/20 * * * *"}

	if got := renderNext(applied, scheduler.StateDrifted, now); got != placeholder {
		t.Errorf("non-applied job: got %q, want placeholder", got)
	}
	if got := renderNext(applied, scheduler.StateApplied, now); got == placeholder {
		t.Errorf("applied job: got placeholder, want a next-fire time")
	}
	// A valid but unreachable schedule (Feb 31) yields a zero time, not an error.
	unreachable := config.Job{Schedule: "0 0 31 2 *"}
	if got := renderNext(unreachable, scheduler.StateApplied, now); got != placeholder {
		t.Errorf("unreachable schedule: got %q, want placeholder", got)
	}
}
