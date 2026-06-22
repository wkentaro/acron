package cli

import (
	"strings"
	"testing"
)

func TestRenderStatusTable(t *testing.T) {
	tbl := statusTable()
	tbl.Row("job-a", "drifted", "success", "2026-01-01 00:00")
	tbl.Row("job-b", "applied", "never run", "") // never-run jobs have no timestamp

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
	for _, header := range []string{"JOB", "APPLY", "LAST RUN", "WHEN"} {
		if !strings.Contains(lines[0], header) {
			t.Errorf("header row %q missing column %q", lines[0], header)
		}
	}
	if len(lines) != 3 {
		t.Errorf("want header + 2 rows = 3 lines, got %d: %q", len(lines), lines)
	}
}
