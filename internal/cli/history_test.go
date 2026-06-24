package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/runner"
)

// markRunning simulates an in-flight Run: it holds the job's lock for the test's
// lifetime and stamps logName as the live log, mirroring how the runner records
// a Run in progress. An empty logName models the Condition-check window, before
// the agent log (and thus the start time) exists.
func markRunning(t *testing.T, job, logName string) {
	t.Helper()
	if err := os.MkdirAll(paths.LocksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(paths.LockPath(job), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte(logName), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	})
}

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
		ts := fmt.Sprintf("2026-06-22T00:%02d:00Z", i)
		records[i] = runner.Record{
			Start: ts, Status: runner.StatusSuccess, DurationS: 1,
			Log: fmt.Sprintf("2026-06-22T00-%02d-00.log", i),
		}
	}
	return records
}

func historyRows(t *testing.T, name string, limit int) []string {
	t.Helper()
	out, err := captureStdout(t, func() error { return runHistory(name, limit) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(out, "\n") {
		t.Errorf("output starts with a blank line: %q", out)
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")[1:] // drop header
}

func TestRunHistoryHeaderColumns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", makeThreeRuns())

	out, err := captureStdout(t, func() error { return runHistory("job", 0) })
	if err != nil {
		t.Fatal(err)
	}
	header := strings.SplitN(out, "\n", 2)[0]
	for _, col := range []string{"JOB", "WHEN", "PASSED", "STATUS", "DURATION"} {
		if !strings.Contains(header, col) {
			t.Errorf("header %q missing column %q", header, col)
		}
	}
}

func TestRunHistoryInterleavedNewestFirst(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "a", "b")
	seedRuns(t, "a", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "a0.log"},
		{Start: "2026-06-22T02:00:00Z", Status: runner.StatusSuccess, Log: "a2.log"},
	})
	seedRuns(t, "b", []runner.Record{
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSuccess, Log: "b1.log"},
		{Start: "2026-06-22T03:00:00Z", Status: runner.StatusSuccess, Log: "b3.log"},
	})

	rows := historyRows(t, "", 0)
	if len(rows) != 4 {
		t.Fatalf("want 4 data rows, got %d: %q", len(rows), rows)
	}
	// b,a,b,a proves the table interleaves by time rather than grouping by job.
	wantJobs := []string{"b", "a", "b", "a"}
	wantTimes := []string{"03:00:00", "02:00:00", "01:00:00", "00:00:00"}
	for i, row := range rows {
		if got := strings.Fields(row)[0]; got != wantJobs[i] {
			t.Errorf("row %d job = %q, want %q", i, got, wantJobs[i])
		}
		if !strings.Contains(row, wantTimes[i]) {
			t.Errorf("row %d = %q, want time %q", i, row, wantTimes[i])
		}
	}
}

func TestRunHistoryShowsRunningRunAtTop(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, DurationS: 5, Log: "2026-06-22T00-00-00.log"},
	})
	markRunning(t, "job", "2026-06-23T00-00-00.log")

	rows := historyRows(t, "job", 0)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (running + finished), got %d: %q", len(rows), rows)
	}
	top := rows[0]
	if !strings.Contains(top, "running") {
		t.Errorf("top row is not the running run: %q", top)
	}
	if !strings.Contains(top, "2026-06-23 00:00:00") {
		t.Errorf("running row missing its start timestamp: %q", top)
	}
	if !strings.Contains(top, "—") {
		t.Errorf("running row should show — for duration: %q", top)
	}
}

func TestRunHistoryDeduplicatesJustFinishedRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-23T00:00:00Z", Status: runner.StatusSuccess, DurationS: 5, Log: "2026-06-23T00-00-00.log"},
	})
	markRunning(t, "job", "2026-06-23T00-00-00.log")

	rows := historyRows(t, "job", 0)
	if len(rows) != 1 {
		t.Fatalf("a just-finished run still holding the lock should not double as a running row, got %d rows: %q", len(rows), rows)
	}
	if strings.Contains(rows[0], "running") {
		t.Errorf("row should show the finished status, not running: %q", rows[0])
	}
}

func TestRunHistoryOmitsRunningRunBeforeStartKnown(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	markRunning(t, "job", "")

	out, err := captureStdout(t, func() error { return runHistory("job", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `No runs for "job"`) {
		t.Errorf("Condition-check run (unknown start) should be omitted, got %q", out)
	}
}

func TestRunHistoryFilterKeepsJobColumnDropsOtherJobs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "first", "second")
	seedRuns(t, "first", makeThreeRuns())
	seedRuns(t, "second", makeThreeRuns())

	out, err := captureStdout(t, func() error { return runHistory("first", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "second") {
		t.Errorf("filtered view leaked the other job: %q", out)
	}
	if !strings.Contains(out, "JOB") || !strings.Contains(out, "first") {
		t.Errorf("filtered view dropped JOB column or rows: %q", out)
	}
}

func TestRunHistoryLimit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", manyRuns(30))

	for _, tc := range []struct{ limit, want int }{{20, 20}, {5, 5}, {0, 30}} {
		if rows := historyRows(t, "job", tc.limit); len(rows) != tc.want {
			t.Errorf("limit %d: got %d rows, want %d", tc.limit, len(rows), tc.want)
		}
	}
}

func TestRunHistorySkippedShowsDashDuration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition},
	})

	out, err := captureStdout(t, func() error { return runHistory("job", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "skipped (condition)") {
		t.Errorf("missing skip status and reason: %q", out)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("skipped run should show — for duration: %q", out)
	}
}

func TestRunHistorySkipWithOutputIsAnnotated(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition, Log: "2026-06-22T00-00-00.log"},
	})

	out, err := captureStdout(t, func() error { return runHistory("job", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "skipped (condition, output)") {
		t.Errorf("a skip carrying a log should be annotated for `acron logs`: %q", out)
	}
}

func TestRunHistoryShowsHumanTimestampsNotFilenames(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, DurationS: 5, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:30:45Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
	})

	human := regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)
	filename := regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}`)
	for _, row := range historyRows(t, "job", 0) {
		if !human.MatchString(row) {
			t.Errorf("row missing human timestamp: %q", row)
		}
		if filename.MatchString(row) {
			t.Errorf("row leaks filename timestamp layout: %q", row)
		}
	}
}

func TestRunHistoryFilteredJobNeverRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")

	out, err := captureStdout(t, func() error { return runHistory("job", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `No runs for "job"`) {
		t.Errorf("got %q", out)
	}
}

func TestRunHistoryAllJobsNoRuns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "a", "b")

	out, err := captureStdout(t, func() error { return runHistory("", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No runs yet") {
		t.Errorf("got %q", out)
	}
}

func TestRunHistoryUnknownJob(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")

	_, err := captureStdout(t, func() error { return runHistory("missing", 0) })
	if err == nil || !strings.Contains(err.Error(), `no job named "missing"`) {
		t.Fatalf("got %v", err)
	}
}

func TestRunHistoryEmptyConfig(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t)

	out, err := captureStdout(t, func() error { return runHistory("", 0) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No jobs in") {
		t.Errorf("got %q", out)
	}
}
