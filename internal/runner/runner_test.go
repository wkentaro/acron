package runner

import (
	"os"
	"testing"

	"github.com/wkentaro/acron/internal/config"
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
	if result.Status != StatusSkipped {
		t.Fatalf("got status=%s, want skipped", result.Status)
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
