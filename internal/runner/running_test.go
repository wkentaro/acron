package runner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/paths"
)

func holdLock(t *testing.T, job string) {
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
	t.Cleanup(func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	})
}

func TestIsRunningNoLockFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if IsRunning("ghost") {
		t.Error("IsRunning = true with no lock file, want false")
	}
}

func TestIsRunningWhileHeld(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	if !IsRunning("busy") {
		t.Error("IsRunning = false while lock held, want true")
	}
}

func TestIsRunningAfterFinish(t *testing.T) {
	job := echoJob(t)
	if _, err := Run(job); err != nil {
		t.Fatal(err)
	}
	if IsRunning(job.Name) {
		t.Error("IsRunning = true after the run finished, want false")
	}
}

func TestRunningSinceReadsInflightLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	if err := os.MkdirAll(paths.RunsDir("busy"), 0o755); err != nil {
		t.Fatal(err)
	}
	logName := "2026-06-22T14-03-00.log"
	if err := os.WriteFile(filepath.Join(paths.RunsDir("busy"), logName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	start, ok := RunningSince("busy")
	if !ok {
		t.Fatal("RunningSince ok = false while running, want true")
	}
	if got := start.Format("2006-01-02 15:04"); got != "2026-06-22 14:03" {
		t.Errorf("start = %q, want %q", got, "2026-06-22 14:03")
	}
}

func TestRunningSinceNotRunning(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, ok := RunningSince("idle"); ok {
		t.Error("RunningSince ok = true when idle, want false")
	}
}

func TestRunningSinceIgnoresRecordedLogs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	if err := appendHistory("busy", Record{Start: time.Now().Format(time.RFC3339), Log: "2026-06-22T13-00-00.log"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RunsDir("busy"), "2026-06-22T13-00-00.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	start, ok := RunningSince("busy")
	if !ok {
		t.Fatal("RunningSince ok = false, want true")
	}
	if !start.IsZero() {
		t.Errorf("start = %v, want zero (only a recorded log exists)", start)
	}
}
