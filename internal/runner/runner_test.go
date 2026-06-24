package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
)

func echoJob(t *testing.T) config.Job {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return config.Job{
		Name:   "echojob",
		Agent:  []string{"/bin/echo", "out:", "{prompt}"},
		Prompt: "hello",
		Cwd:    t.TempDir(),
	}
}

func TestRunSuccess(t *testing.T) {
	job := echoJob(t)

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSuccess || result.Exit != 0 {
		t.Fatalf("got status=%s exit=%d", result.Status, result.Exit)
	}

	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "out: hello\n" {
		t.Errorf("log = %q, want %q", got, "out: hello\n")
	}

	records, err := History(job.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != StatusSuccess {
		t.Errorf("history = %+v", records)
	}
}

func TestRunFailureRecordsExit(t *testing.T) {
	job := echoJob(t)
	job.Agent = []string{"/bin/sh", "-c", "exit 3"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFailure || result.Exit != 3 {
		t.Fatalf("got status=%s exit=%d, want failure/3", result.Status, result.Exit)
	}
}

func TestRunPassesJobEnvToAgent(t *testing.T) {
	job := echoJob(t)
	job.Agent = []string{"/bin/sh", "-c", "echo $ACRON_TEST_VAR"}
	t.Setenv("ACRON_TEST_VAR", "inherited")
	job.Env = map[string]string{"ACRON_TEST_VAR": "sentinel"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("got status=%s, want success", result.Status)
	}

	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "sentinel\n" {
		t.Errorf("log = %q, want %q", got, "sentinel\n")
	}
}

func TestRunSkipsWhenLocked(t *testing.T) {
	job := echoJob(t)

	lock, held, err := acquireLock(job.Name)
	if err != nil || !held {
		t.Fatalf("expected to hold lock: held=%v err=%v", held, err)
	}
	defer releaseLock(lock)

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || result.Reason != ReasonOverlap {
		t.Fatalf("got status=%s reason=%s, want skipped/overlap", result.Status, result.Reason)
	}
}

func TestRunTimeout(t *testing.T) {
	job := echoJob(t)
	job.Agent = []string{"/bin/sh", "-c", "sleep 30"}
	job.Timeout = "1s"

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusTimeout {
		t.Fatalf("got status=%s, want timeout", result.Status)
	}
}

func TestRetentionKeepsLast50(t *testing.T) {
	job := echoJob(t)
	for i := 0; i < keepRuns+5; i++ {
		_ = appendHistory(job.Name, Record{Status: StatusSuccess})
	}
	trimHistory(job.Name)
	records, err := History(job.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != keepRuns {
		t.Errorf("history kept %d records, want %d", len(records), keepRuns)
	}
}

func TestRunConditionProceeds(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "exit 0"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSuccess || result.Reason != ReasonNone {
		t.Fatalf("got status=%s reason=%s, want success/none", result.Status, result.Reason)
	}
	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "out: hello\n" {
		t.Errorf("log = %q, want %q", got, "out: hello\n")
	}
}

func TestRunConditionPassPrintsMarker(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "exit 0"}

	out := captureStdout(t, func() {
		if _, err := Run(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	})

	marker := "condition passed " + job.Name
	markerAt := strings.Index(out, marker)
	agentAt := strings.Index(out, "out: hello")
	if markerAt < 0 || agentAt < 0 || markerAt > agentAt {
		t.Errorf("want marker before agent output in stdout = %q", out)
	}
}

func TestRunNoConditionOmitsMarker(t *testing.T) {
	job := echoJob(t)

	out := captureStdout(t, func() {
		if _, err := Run(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(out, "condition passed") {
		t.Errorf("stdout = %q, want no condition marker", out)
	}
}

func TestRunConditionSkips(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "exit 1"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || result.Reason != ReasonCondition {
		t.Fatalf("got status=%s reason=%s, want skipped/condition", result.Status, result.Reason)
	}
	if result.LogPath != "" {
		t.Errorf("clean skip wrote a log: %s", result.LogPath)
	}

	records, err := History(job.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != StatusSkipped || records[0].Reason != ReasonCondition {
		t.Errorf("history = %+v", records)
	}
	if logs := logFiles(t, job.Name); len(logs) != 0 {
		t.Errorf("clean skip left log files: %v", logs)
	}
}

func TestRunConditionSkipWithStdoutOnlyStaysClean(t *testing.T) {
	job := echoJob(t)
	// A chatty-but-working gate, e.g. `jq -e 'length > 0'` printing `false` on
	// the negative case: stdout output, no stderr, an ordinary skip.
	job.Condition = []string{"/bin/sh", "-c", "echo false; exit 1"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || result.Reason != ReasonCondition {
		t.Fatalf("got status=%s reason=%s, want skipped/condition", result.Status, result.Reason)
	}
	if result.LogPath != "" {
		t.Errorf("stdout-only skip wrote a log: %s", result.LogPath)
	}
	if logs := logFiles(t, job.Name); len(logs) != 0 {
		t.Errorf("stdout-only skip left log files: %v", logs)
	}
}

func TestRunConditionSkipWithStderrWritesLog(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "echo gh auth login >&2; exit 2"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || result.Reason != ReasonCondition {
		t.Fatalf("got status=%s reason=%s, want skipped/condition", result.Status, result.Reason)
	}

	last, ok, err := LastRecord(job.Name)
	if err != nil || !ok {
		t.Fatalf("LastRecord ok=%v err=%v", ok, err)
	}
	if last.Log == "" {
		t.Fatal("skip with output left no log reference on the record")
	}
	if last.Exit != 2 {
		t.Errorf("recorded exit = %d, want 2 (the broken condition's exit)", last.Exit)
	}

	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "gh auth login") {
		t.Errorf("log = %q, want it to contain the condition output", string(data))
	}
}

func TestRunConditionFailureWritesLog(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "echo broke >&2; exit 255"}

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFailure || result.Reason != ReasonCondition || result.Exit != 255 {
		t.Fatalf("got status=%s reason=%s exit=%d, want failure/condition/255",
			result.Status, result.Reason, result.Exit)
	}
	data, err := os.ReadFile(result.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "broke") {
		t.Errorf("log = %q, want it to contain the condition output", string(data))
	}
}

func TestRunConditionTimeoutFails(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "sleep 30"}
	job.Timeout = "1s"

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFailure || result.Reason != ReasonCondition {
		t.Fatalf("got status=%s reason=%s, want failure/condition", result.Status, result.Reason)
	}
}

func TestRunConditionOverlapTakesPrecedence(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "exit 0"}

	lock, held, err := acquireLock(job.Name)
	if err != nil || !held {
		t.Fatalf("expected to hold lock: held=%v err=%v", held, err)
	}
	defer releaseLock(lock)

	result, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || result.Reason != ReasonOverlap {
		t.Fatalf("got status=%s reason=%s, want skipped/overlap", result.Status, result.Reason)
	}
}

func TestRunAgentInterrupted(t *testing.T) {
	job := echoJob(t)
	job.Agent = []string{"/bin/sh", "-c", "sleep 30"}

	ctx, stop := cancelWhenRunning(t, job.Name)
	defer stop()

	result, err := Run(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInterrupted {
		t.Fatalf("got status=%s, want interrupted", result.Status)
	}
	if IsRunning(job.Name) {
		t.Error("lock still held after interrupt, want released")
	}
}

func TestRunAgentInterruptedWhenSignalTrapped(t *testing.T) {
	job := echoJob(t)
	// An agent that traps the signal and exits 0 still aborted at the operator's
	// hand: the cancellation is recorded as interrupted, not as a clean success.
	// The inner sleep's output is detached from the captured pipe so the shell's
	// trapped exit tears the run down at once instead of waiting out killGrace.
	job.Agent = []string{"/bin/sh", "-c", "trap 'exit 0' TERM; sleep 30 >/dev/null 2>&1"}

	ctx, stop := cancelWhenRunning(t, job.Name)
	defer stop()

	result, err := Run(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInterrupted {
		t.Fatalf("got status=%s, want interrupted", result.Status)
	}
}

func TestRunConditionInterrupted(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "sleep 30"}

	ctx, stop := cancelWhenRunning(t, job.Name)
	defer stop()

	result, err := Run(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInterrupted || result.Reason != ReasonCondition {
		t.Fatalf("got status=%s reason=%s, want interrupted/condition", result.Status, result.Reason)
	}
	if IsRunning(job.Name) {
		t.Error("lock still held after interrupt, want released")
	}
	if result.Command != nil {
		t.Errorf("agent ran despite the condition being interrupted: %v", result.Command)
	}
}

// cancelWhenRunning returns a context that cancels once a Run holds job's lock,
// simulating a Ctrl-C landing while the run is in flight, and a stop that ends
// the watcher. It polls the lock because the runner exposes no synchronous seam
// to signal that the child has started.
func cancelWhenRunning(t *testing.T, job string) (context.Context, func()) {
	t.Helper()
	const lockPollInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if IsRunning(job) {
				cancel()
				return
			}
			time.Sleep(lockPollInterval)
		}
	}()
	return ctx, cancel
}

func TestRetentionSkipsDoNotEvictRealRuns(t *testing.T) {
	job := echoJob(t)
	for i := 0; i < 3; i++ {
		_ = appendHistory(job.Name, Record{Status: StatusSuccess})
	}
	for i := 0; i < keepRuns+5; i++ {
		_ = appendHistory(job.Name, Record{Status: StatusSkipped, Reason: ReasonCondition})
	}
	trimHistory(job.Name)

	records, err := History(job.Name)
	if err != nil {
		t.Fatal(err)
	}
	real, skip := 0, 0
	for _, rec := range records {
		if rec.Status == StatusSkipped {
			skip++
		} else {
			real++
		}
	}
	if real != 3 {
		t.Errorf("kept %d real runs, want 3 (skips must not evict them)", real)
	}
	if skip != keepRuns {
		t.Errorf("kept %d skips, want %d", skip, keepRuns)
	}
}

func TestPruneRunsKeepsRealLogsDespiteSkipFlood(t *testing.T) {
	job := echoJob(t)
	runsDir := paths.RunsDir(job.Name)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRun := func(status Status, log string) {
		if err := os.WriteFile(filepath.Join(runsDir, log), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = appendHistory(job.Name, Record{Status: status, Log: log})
	}

	var realLogs []string
	for i := 0; i < keepRuns; i++ {
		name := fmt.Sprintf("real-%02d.log", i)
		realLogs = append(realLogs, name)
		writeRun(StatusSuccess, name)
	}
	for i := 0; i < keepRuns+5; i++ {
		writeRun(StatusSkipped, fmt.Sprintf("skip-%02d.log", i))
	}

	pruneRuns(job.Name)

	for _, name := range realLogs {
		if _, err := os.Stat(filepath.Join(runsDir, name)); err != nil {
			t.Errorf("real-run log %s evicted by skip-log flood: %v", name, err)
		}
	}
	if got := len(logFiles(t, job.Name)); got != keepRuns*2 {
		t.Errorf("kept %d log files, want %d (50 real + 50 skip)", got, keepRuns*2)
	}
}

func TestPruneRunsKeepsLogsWhenHistoryMissing(t *testing.T) {
	job := echoJob(t)
	runsDir := paths.RunsDir(job.Name)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runsDir, "orphan.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	pruneRuns(job.Name)

	if logs := logFiles(t, job.Name); len(logs) != 1 {
		t.Errorf("pruned logs with no history present: kept %v, want [orphan.log]", logs)
	}
}

func TestRecordSkippedPrunesEvictedSkipLog(t *testing.T) {
	job := echoJob(t)
	runsDir := paths.RunsDir(job.Name)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fill the skip bucket to its cap with logged condition-skips on disk.
	for i := 0; i < keepRuns; i++ {
		log := fmt.Sprintf("skip-%02d.log", i)
		if err := os.WriteFile(filepath.Join(runsDir, log), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_ = appendHistory(job.Name, Record{Status: StatusSkipped, Reason: ReasonCondition, Log: log})
	}
	oldest := filepath.Join(runsDir, "skip-00.log")

	// One more logless skip evicts the oldest logged skip from history; its log
	// must be pruned, not orphaned on disk.
	if _, err := recordSkipped(job.Name, ReasonOverlap); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Errorf("evicted skip log was not pruned: stat err = %v", err)
	}
}

func TestPruneRunsKeepsInflightLiveLog(t *testing.T) {
	job := echoJob(t)
	runsDir := paths.RunsDir(job.Name)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A prior recorded run, so pruneRuns does not bail on an empty kept set.
	if err := os.WriteFile(filepath.Join(runsDir, "done.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = appendHistory(job.Name, Record{Status: StatusSuccess, Log: "done.log"})

	// An in-flight Run: its log is on disk and stamped live in the lock file, but
	// its history record does not exist yet (finishRun has not run).
	live := "2026-06-24T12-00-00.log"
	if err := os.WriteFile(filepath.Join(runsDir, live), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.LocksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	stampLiveLog(t, job.Name, live)

	// A concurrent overlap-skip prunes; it must not delete the running agent's log.
	if _, err := recordSkipped(job.Name, ReasonOverlap); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(runsDir, live)); err != nil {
		t.Errorf("pruneRuns deleted the in-flight live log: %v", err)
	}
}

// captureStdout swaps os.Stdout for a pipe while fn runs and returns what fn
// wrote. The swap is process-global, so callers must not run in parallel. fn's
// output must stay under the pipe buffer (these tests emit a few bytes), since
// it is drained only after fn returns.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = orig
		_ = r.Close()
	})
	fn()
	_ = w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func logFiles(t *testing.T, job string) []string {
	t.Helper()
	entries, err := os.ReadDir(paths.RunsDir(job))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var logs []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".log") {
			logs = append(logs, entry.Name())
		}
	}
	return logs
}
