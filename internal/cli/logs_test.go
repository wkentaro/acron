package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/runner"
)

// Pin the whole cli test binary to UTC. Display and selector parsing both go
// through time.Local; without a fixed zone the UTC record starts and the
// local-wall-clock selectors would diverge by the host offset, making the
// display and round-trip assertions host-dependent.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

func seedRuns(t *testing.T, job string, records []runner.Record) {
	t.Helper()
	dir := paths.RunsDir(job)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var history strings.Builder
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		history.Write(line)
		history.WriteByte('\n')
		if rec.Log != "" {
			path := filepath.Join(dir, rec.Log)
			if err := os.WriteFile(path, []byte("output of "+rec.Log), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(paths.HistoryPath(job), []byte(history.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	out, _, err := captureOutErr(t, fn)
	return out, err
}

func makeThreeRuns() []runner.Record {
	return []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusFailure, Log: "2026-06-22T01-00-00.log"},
		{Start: "2026-06-22T02:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T02-00-00.log"},
	}
}

func TestResolveLogLatestPicksNewest(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	for _, selector := range []string{"", "latest"} {
		rec, _, err := resolveLog("job", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if rec.Log != "2026-06-22T02-00-00.log" {
			t.Errorf("selector %q: got %q", selector, rec.Log)
		}
	}
}

func TestResolveLogLatestSkipsRunsWithoutOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
	})

	rec, _, err := resolveLog("job", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Log != "2026-06-22T00-00-00.log" {
		t.Errorf("got %q", rec.Log)
	}
}

func TestResolveLogLatestSurfacesConditionSkipOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition, Log: "2026-06-22T01-00-00.log"},
	})

	rec, _, err := resolveLog("job", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Log != "2026-06-22T01-00-00.log" {
		t.Errorf("got %q, want the broken condition's output surfaced as the latest log", rec.Log)
	}
}

func TestResolveLogAllSkipped(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition},
	})

	_, _, err := resolveLog("job", "latest")
	if err == nil || !strings.Contains(err.Error(), "no captured output") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogByTimestamp(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	for _, selector := range []string{"2026-06-22T01-00-00", "2026-06-22 01:00:00"} {
		rec, _, err := resolveLog("job", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if rec.Log != "2026-06-22T01-00-00.log" {
			t.Errorf("selector %q: got %q", selector, rec.Log)
		}
	}
}

func TestResolveLogByTimestampSkippedRunHasNoOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
	})

	_, _, err := resolveLog("job", "2026-06-22 00:00:00")
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogTimestampNoMatch(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	_, _, err := resolveLog("job", "2026-01-01T00-00-00")
	if err == nil || !strings.Contains(err.Error(), "no run") {
		t.Fatalf("got %v", err)
	}
}

// A bare ordinal like "3" is no longer a selector; it falls through to timestamp
// parsing and is rejected, rather than picking the 3rd most recent run.
func TestResolveLogRejectsNonTimestampSelector(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	_, _, err := resolveLog("job", "3")
	if err == nil || !strings.Contains(err.Error(), "unrecognized timestamp") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogNoRuns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, _, err := resolveLog("job", "")
	if err == nil || !strings.Contains(err.Error(), "no runs") {
		t.Fatalf("got %v", err)
	}
}

func TestRunLogsCopiesOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", makeThreeRuns())

	out, err := captureStdout(t, func() error { return runLogs("job", "2026-06-22 01:00:00") })
	if err != nil {
		t.Fatal(err)
	}
	if out != "output of 2026-06-22T01-00-00.log" {
		t.Errorf("got %q", out)
	}
}

func TestRunLogsSummaryToStderrLeavesStdoutClean(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T02:00:00Z", Status: runner.StatusSuccess, Exit: 0, DurationS: 252, Log: "2026-06-22T02-00-00.log"},
	})

	out, errOut, err := captureOutErr(t, func() error { return runLogs("job", "latest") })
	if err != nil {
		t.Fatal(err)
	}
	if out != "output of 2026-06-22T02-00-00.log" {
		t.Errorf("stdout not the pure log body: %q", out)
	}
	for _, want := range []string{"job", "2026-06-22 02:00:00", "success", "4min 12s"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr summary %q missing %q", errOut, want)
		}
	}
}

func TestResolveLogTimestampResolvesRunningRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	markRunning(t, "job", "2026-06-23T00-00-00.log")

	for _, selector := range []string{"2026-06-23T00-00-00", "2026-06-23 00:00:00"} {
		rec, running, err := resolveLog("job", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if !running {
			t.Errorf("selector %q: running = false, want true", selector)
		}
		if rec.Log != "2026-06-23T00-00-00.log" {
			t.Errorf("selector %q: got %q", selector, rec.Log)
		}
	}
}

func TestResolveLogLatestIgnoresRunningRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	markRunning(t, "job", "2026-06-23T00-00-00.log")

	_, _, err := resolveLog("job", "latest")
	if err == nil || !strings.Contains(err.Error(), "no runs") {
		t.Fatalf("latest should stay on finished output, got %v", err)
	}
}

func TestRunLogsTailsRunningRun(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	dir := paths.RunsDir("job")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-06-23T00-00-00.log"), []byte("partial output"), 0o644); err != nil {
		t.Fatal(err)
	}
	markRunning(t, "job", "2026-06-23T00-00-00.log")

	out, errOut, err := captureOutErr(t, func() error { return runLogs("job", "2026-06-23 00:00:00") })
	if err != nil {
		t.Fatal(err)
	}
	if out != "partial output" {
		t.Errorf("stdout = %q, want the partial live log", out)
	}
	if !strings.Contains(errOut, "running") {
		t.Errorf("summary should mark the run running: %q", errOut)
	}
}

func TestRunLogsUnknownJob(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")

	err := runLogs("nope", "")
	if err == nil || !strings.Contains(err.Error(), `no job named "nope"`) {
		t.Fatalf("got %v", err)
	}
}

func TestRunLogsConfiguredJobNoRuns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")

	err := runLogs("job", "")
	if err == nil || !strings.Contains(err.Error(), "no runs") {
		t.Fatalf("got %v", err)
	}
}
