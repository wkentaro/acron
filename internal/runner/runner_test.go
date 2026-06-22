package runner

import (
	"os"
	"strings"
	"testing"

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

	result, err := Run(job)
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

	result, err := Run(job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFailure || result.Exit != 3 {
		t.Fatalf("got status=%s exit=%d, want failure/3", result.Status, result.Exit)
	}
}

func TestRunSkipsWhenLocked(t *testing.T) {
	job := echoJob(t)

	lock, held, err := acquireLock(job.Name)
	if err != nil || !held {
		t.Fatalf("expected to hold lock: held=%v err=%v", held, err)
	}
	defer releaseLock(lock)

	result, err := Run(job)
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

	result, err := Run(job)
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

	result, err := Run(job)
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

func TestRunConditionSkips(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "exit 1"}

	result, err := Run(job)
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

func TestRunConditionFailureWritesLog(t *testing.T) {
	job := echoJob(t)
	job.Condition = []string{"/bin/sh", "-c", "echo broke >&2; exit 255"}

	result, err := Run(job)
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

	result, err := Run(job)
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

	result, err := Run(job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped || result.Reason != ReasonOverlap {
		t.Fatalf("got status=%s reason=%s, want skipped/overlap", result.Status, result.Reason)
	}
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
