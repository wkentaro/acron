package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/runner"
)

func acquireTestLock(t *testing.T, job string) *os.File {
	t.Helper()
	if err := os.MkdirAll(paths.LocksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(paths.LockPath(job), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return lock
}

func stampLiveLog(t *testing.T, job, logName string) {
	t.Helper()
	if err := os.WriteFile(paths.LockPath(job), []byte(logName), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHistory(t *testing.T, job string, records ...runner.Record) {
	t.Helper()
	if err := os.MkdirAll(paths.RunsDir(job), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(paths.HistoryPath(job), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func captureOutErr(t *testing.T, fn func() error) (stdout, stderr string, runErr error) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr = origOut, origErr })
	runErr = fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = origOut, origErr
	out, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := io.ReadAll(errR)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), string(errOut), runErr
}

func TestRunFollowRejectsRunSelector(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	err := runFollow("job", "3")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with a run selector") {
		t.Fatalf("got %v", err)
	}
}

func TestRunFollowUnknownJob(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "known")
	err := runFollow("typo", "")
	if err == nil || !strings.Contains(err.Error(), `no job named "typo"`) {
		t.Fatalf("got %v", err)
	}
}

func TestRunFollowNoRunInProgress(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "job")
	err := runFollow("job", "")
	if err == nil || !strings.Contains(err.Error(), "no run in progress") {
		t.Fatalf("got %v", err)
	}
}

func TestRunFollowStreamsLiveRunToCompletion(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "busy")
	job, logName := "busy", "2026-06-22T02-00-00.log"
	logPath := filepath.Join(paths.RunsDir(job), logName)
	// Pre-seed the record the runner writes at Run end (the footer reads it),
	// plus a later record for an unrelated Run to prove the footer is resolved
	// by the streamed log name, not by "last record".
	writeHistory(
		t, job,
		runner.Record{Start: "2026-06-22T02:00:00Z", Status: runner.StatusSuccess, DurationS: 5, Log: logName},
		runner.Record{Start: "2026-06-22T03:00:00Z", Status: runner.StatusFailure, Exit: 1, DurationS: 9, Log: "2026-06-22T03-00-00.log"},
	)
	if err := os.WriteFile(logPath, []byte("hello "), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := acquireTestLock(t, job)
	stampLiveLog(t, job, logName)

	go func() {
		time.Sleep(50 * time.Millisecond)
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString("world")
			_ = f.Close()
		}
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	out, errOut, err := captureOutErr(t, func() error { return runFollow(job, "") })
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello world" {
		t.Errorf("stdout = %q, want %q", out, "hello world")
	}
	if !strings.Contains(errOut, "run success in 5s") {
		t.Errorf("stderr footer = %q, want it to report success", errOut)
	}
}

func TestRunFollowDegradesWhenRunEndsDuringCondition(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	seedConfig(t, "busy")
	job := "busy"
	writeHistory(t, job, runner.Record{
		Start: "2026-06-22T02:00:00Z", Status: runner.StatusSkipped, Reason: runner.ReasonCondition,
	})

	lock := acquireTestLock(t, job) // held, no live log stamped: condition phase

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	out, errOut, err := captureOutErr(t, func() error { return runFollow(job, "") })
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty (skipped run has no output)", out)
	}
	if !strings.Contains(errOut, "run skipped (condition)") {
		t.Errorf("stderr footer = %q, want it to report the skip", errOut)
	}
}

func TestFollowFooter(t *testing.T) {
	cases := []struct {
		name string
		rec  runner.Record
		want string
	}{
		{"success", runner.Record{Status: runner.StatusSuccess, DurationS: 182}, "run success in 3m2s"},
		{"failure", runner.Record{Status: runner.StatusFailure, Exit: 1, DurationS: 252}, "run failure (exit 1) in 4m12s"},
		{"timeout", runner.Record{Status: runner.StatusTimeout, Exit: -1, DurationS: 1800}, "run timeout in 30m0s"},
		{"condition failure", runner.Record{Status: runner.StatusFailure, Reason: runner.ReasonCondition, Exit: 2}, "run failure (condition, exit 2) in 0s"},
		{"skipped", runner.Record{Status: runner.StatusSkipped, Reason: runner.ReasonCondition}, "run skipped (condition)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := followFooter(tc.rec); got != tc.want {
				t.Errorf("followFooter = %q, want %q", got, tc.want)
			}
		})
	}
}
