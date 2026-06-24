package runner

import (
	"context"
	"os"
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

func stampLiveLog(t *testing.T, job, logName string) {
	t.Helper()
	if err := os.WriteFile(paths.LockPath(job), []byte(logName), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if _, err := Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if IsRunning(job.Name) {
		t.Error("IsRunning = true after the run finished, want false")
	}
}

func TestRunningSinceReadsInflightLog(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	stampLiveLog(t, "busy", "2026-06-22T14-03-00.log")
	start, ok := RunningSince("busy")
	if !ok {
		t.Fatal("RunningSince ok = false while running, want true")
	}
	if got := start.Format("2006-01-02 15:04"); got != "2026-06-22 14:03" {
		t.Errorf("start = %q, want %q", got, "2026-06-22 14:03")
	}
	// The name is stamped in local time, so the parsed instant must be local,
	// not UTC: an elapsed-time computation against time.Now() depends on it.
	if want := time.Date(2026, 6, 22, 14, 3, 0, 0, time.Local); !start.Equal(want) {
		t.Errorf("start instant = %v, want %v (parsed in local time)", start, want)
	}
}

func TestInFlightStreaming(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	stampLiveLog(t, "busy", "2026-06-22T14-03-00.log")
	logName, running := InFlight("busy")
	if !running {
		t.Fatal("InFlight running = false while lock held, want true")
	}
	if logName != "2026-06-22T14-03-00.log" {
		t.Errorf("InFlight logName = %q, want the stamped log", logName)
	}
}

func TestInFlightDuringCondition(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	logName, running := InFlight("busy")
	if !running {
		t.Fatal("InFlight running = false during condition check, want true")
	}
	if logName != "" {
		t.Errorf("InFlight logName = %q, want empty (agent log not yet created)", logName)
	}
}

func TestInFlightIdle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, running := InFlight("idle"); running {
		t.Error("InFlight running = true when idle, want false")
	}
}

// TestLiveLogLifecycle drives the real lock-file write path: acquireLock clears
// a stale name, recordLiveLog stamps via WriteAt on the held fd, and releaseLock
// clears it again so the next Run never sees this one's name.
func TestLiveLogLifecycle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(paths.LocksDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LockPath("job"), []byte("2025-01-01T00-00-00.log"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock, held, err := acquireLock("job")
	if err != nil || !held {
		t.Fatalf("acquireLock held=%v err=%v", held, err)
	}
	if got := liveLogName("job"); got != "" {
		t.Errorf("after acquire: liveLogName = %q, want empty (stale name cleared)", got)
	}

	recordLiveLog(lock, "2026-06-22T05-00-00.log")
	if got := liveLogName("job"); got != "2026-06-22T05-00-00.log" {
		t.Errorf("after stamp: liveLogName = %q, want the stamped name", got)
	}

	releaseLock(lock)
	if got := liveLogName("job"); got != "" {
		t.Errorf("after release: liveLogName = %q, want empty", got)
	}
}

func TestRunningSinceNotRunning(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, ok := RunningSince("idle"); ok {
		t.Error("RunningSince ok = true when idle, want false")
	}
}

func TestRunningSinceDuringCondition(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	holdLock(t, "busy")
	start, ok := RunningSince("busy")
	if !ok {
		t.Fatal("RunningSince ok = false during condition check, want true")
	}
	if !start.IsZero() {
		t.Errorf("start = %v, want zero (agent log not yet created)", start)
	}
}
