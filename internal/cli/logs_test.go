package cli

import (
	"encoding/json"
	"io"
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
	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = original })
	runErr := fn()
	_ = w.Close()
	os.Stdout = original
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
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
		name, err := resolveLog("job", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if name != "2026-06-22T02-00-00.log" {
			t.Errorf("selector %q: got %q", selector, name)
		}
	}
}

func TestResolveLogByIndex(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	cases := map[string]string{
		"1": "2026-06-22T02-00-00.log",
		"2": "2026-06-22T01-00-00.log",
		"3": "2026-06-22T00-00-00.log",
	}
	for selector, want := range cases {
		name, err := resolveLog("job", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if name != want {
			t.Errorf("selector %q: got %q, want %q", selector, name, want)
		}
	}
}

func TestResolveLogIndexOutOfRange(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	_, err := resolveLog("job", "4")
	if err == nil || !strings.Contains(err.Error(), "no run 4") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogSkippedIndexReportsSkip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition},
	})

	_, err := resolveLog("job", "1")
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogLatestSkipsRunsWithoutOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSuccess, Log: "2026-06-22T00-00-00.log"},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
	})

	name, err := resolveLog("job", "latest")
	if err != nil {
		t.Fatal(err)
	}
	if name != "2026-06-22T00-00-00.log" {
		t.Errorf("got %q", name)
	}
}

func TestResolveLogAllSkipped(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
		{Start: "2026-06-22T01:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition},
	})

	_, err := resolveLog("job", "latest")
	if err == nil || !strings.Contains(err.Error(), "no captured output") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogByTimestamp(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	for _, selector := range []string{"2026-06-22T01-00-00", "2026-06-22 01:00:00"} {
		name, err := resolveLog("job", selector)
		if err != nil {
			t.Fatalf("selector %q: %v", selector, err)
		}
		if name != "2026-06-22T01-00-00.log" {
			t.Errorf("selector %q: got %q", selector, name)
		}
	}
}

func TestResolveLogByTimestampSkippedRunHasNoOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", []runner.Record{
		{Start: "2026-06-22T00:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonOverlap},
	})

	_, err := resolveLog("job", "2026-06-22 00:00:00")
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogUnknownTimestamp(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedRuns(t, "job", makeThreeRuns())

	_, err := resolveLog("job", "2026-01-01T00-00-00")
	if err == nil || !strings.Contains(err.Error(), "no run") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveLogNoRuns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	_, err := resolveLog("job", "")
	if err == nil || !strings.Contains(err.Error(), "no runs") {
		t.Fatalf("got %v", err)
	}
}

func TestRunLogsCopiesOutput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	seedRuns(t, "job", makeThreeRuns())

	out, err := captureStdout(t, func() error { return runLogs("job", "2") })
	if err != nil {
		t.Fatal(err)
	}
	if out != "output of 2026-06-22T01-00-00.log" {
		t.Errorf("got %q", out)
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
