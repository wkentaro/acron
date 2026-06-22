package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wkentaro/acron/internal/runner"
)

func seedConfig(t *testing.T, jobs ...string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	for _, name := range jobs {
		fmt.Fprintf(&b, "[[job]]\nname = %q\nschedule = \"* * * * *\"\nagent = [\"echo\"]\nprompt = \"hi\"\ncwd = %q\n\n", name, dir)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACRON_CONFIG", path)
}

func manyRuns(n int) []runner.Record {
	records := make([]runner.Record, n)
	for i := range records {
		ts := fmt.Sprintf("2026-06-22T%02d:00:00Z", i)
		records[i] = runner.Record{Start: ts, Status: runner.StatusSuccess, Log: ts[:13] + "-00-00.log"}
	}
	return records
}

func TestRunHistoryNumbersNewestFirst(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", makeThreeRuns())

	out, err := captureStdout(t, func() error { return runHistory("job") })
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(out, "\n") {
		t.Errorf("output starts with a blank line: %q", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "job" {
		t.Errorf("first line = %q, want job header", lines[0])
	}
	body := lines[len(lines)-3:]
	if got := strings.Fields(body[0]); got[0] != "1" || !strings.Contains(body[0], "2026-06-22 02:00:00") {
		t.Errorf("first run row = %q", body[0])
	}
	if got := strings.Fields(body[2]); got[0] != "3" || !strings.Contains(body[2], "2026-06-22 00:00:00") {
		t.Errorf("last run row = %q", body[2])
	}
}

func TestRunHistoryFormatsCapturedAndSkippedUniformly(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:30:45Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
	})

	out, err := captureStdout(t, func() error { return runHistory("job") })
	if err != nil {
		t.Fatal(err)
	}

	human := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
	filename := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}`)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, row := range lines[len(lines)-2:] {
		if !human.MatchString(row) {
			t.Errorf("row missing human timestamp: %q", row)
		}
		if filename.MatchString(row) {
			t.Errorf("row leaks filename timestamp layout: %q", row)
		}
	}
}

func TestRunHistoryNeverRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")

	out, err := captureStdout(t, func() error { return runHistory("job") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "job") || !strings.Contains(out, "never run") {
		t.Errorf("got %q", out)
	}
}

func TestRunHistoryUnknownJob(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")

	_, err := captureStdout(t, func() error { return runHistory("missing") })
	if err == nil || !strings.Contains(err.Error(), `no job named "missing"`) {
		t.Fatalf("got %v", err)
	}
}

func TestRunHistoryAllJobs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "first", "second")
	seedRuns(t, "first", makeThreeRuns())

	out, err := captureStdout(t, func() error { return runHistory("") })
	if err != nil {
		t.Fatal(err)
	}
	firstAt := strings.Index(out, "first")
	secondAt := strings.Index(out, "second")
	if firstAt < 0 || secondAt < 0 {
		t.Fatalf("missing a job section: %q", out)
	}
	if firstAt > secondAt {
		t.Errorf("sections out of config order: %q", out)
	}
	if !strings.Contains(out, "never run") {
		t.Errorf("second job should show never run: %q", out)
	}
}

func TestRunHistoryEmptyConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t)

	out, err := captureStdout(t, func() error { return runHistory("") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No jobs in") {
		t.Errorf("got %q", out)
	}
}

func TestRunHistoryGlobalIndexWidth(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "wide", "narrow")
	seedRuns(t, "wide", manyRuns(10))
	seedRuns(t, "narrow", makeThreeRuns()[:1])

	out, err := captureStdout(t, func() error { return runHistory("") })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	labelColumn := func(header string) int {
		for i, line := range lines {
			if line == header {
				return strings.Index(lines[i+1], "2026")
			}
		}
		t.Fatalf("no section for %q in %q", header, out)
		return -1
	}
	if labelColumn("wide") != labelColumn("narrow") {
		t.Errorf("index widths not aligned across sections: %q", out)
	}
}
