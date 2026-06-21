package runner

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
)

const (
	timestampLayout = "2006-01-02T15-04-05"
	killGrace       = 10 * time.Second
	keepRuns        = 50
)

type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
	StatusTimeout Status = "timeout"
	StatusSkipped Status = "skipped"
)

type Result struct {
	Status   Status
	Exit     int
	Duration time.Duration
	LogPath  string
}

type Record struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Status    Status `json:"status"`
	Exit      int    `json:"exit"`
	DurationS int    `json:"duration_s"`
	Log       string `json:"log"`
}

func Run(job config.Job) (Result, error) {
	timeout, err := job.ResolvedTimeout()
	if err != nil {
		return Result{}, err
	}

	lock, held, err := acquireLock(job.Name)
	if err != nil {
		return Result{}, err
	}
	if !held {
		return recordSkipped(job.Name)
	}
	defer releaseLock(lock)

	runsDir := paths.RunsDir(job.Name)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return Result{}, err
	}

	start := time.Now()
	logName := start.Format(timestampLayout) + ".log"
	logPath := filepath.Join(runsDir, logName)
	logFile, err := os.Create(logPath)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = logFile.Close() }()

	exit, status := execAgent(job, timeout, io.MultiWriter(logFile, os.Stdout))
	duration := time.Since(start)

	rec := Record{
		Start:     start.Format(time.RFC3339),
		End:       start.Add(duration).Format(time.RFC3339),
		Status:    status,
		Exit:      exit,
		DurationS: int(duration.Seconds()),
		Log:       logName,
	}
	if err := appendHistory(job.Name, rec); err != nil {
		return Result{}, err
	}
	pruneRuns(job.Name)

	return Result{Status: status, Exit: exit, Duration: duration, LogPath: logPath}, nil
}

func recordSkipped(job string) (Result, error) {
	now := time.Now().Format(time.RFC3339)
	if err := appendHistory(job, Record{Start: now, End: now, Status: StatusSkipped}); err != nil {
		return Result{}, err
	}
	trimHistory(job)
	return Result{Status: StatusSkipped}, nil
}

func execAgent(job config.Job, timeout time.Duration, out io.Writer) (int, Status) {
	ctx := context.Background()
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	argv := substitutePrompt(job.Agent, job.Prompt)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = paths.ExpandHome(job.Cwd)
	cmd.Env = jobEnv(job)
	cmd.Stdin = nil // nil stdin connects the child to /dev/null
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = killGrace // SIGKILL if it ignores SIGTERM within the grace period

	err := cmd.Run()
	switch {
	case err == nil:
		return 0, StatusSuccess
	case ctx.Err() == context.DeadlineExceeded:
		return -1, StatusTimeout
	default:
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), StatusFailure
		}
		return -1, StatusFailure
	}
}

func acquireLock(job string) (*os.File, bool, error) {
	if err := os.MkdirAll(paths.LocksDir(), 0o755); err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile(paths.LockPath(job), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return file, true, nil
}

func releaseLock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func substitutePrompt(agent []string, prompt string) []string {
	argv := make([]string, 0, len(agent)+1)
	replaced := false
	for _, token := range agent {
		if token == "{prompt}" {
			argv = append(argv, prompt)
			replaced = true
			continue
		}
		argv = append(argv, token)
	}
	if !replaced {
		argv = append(argv, prompt)
	}
	return argv
}

func jobEnv(job config.Job) []string {
	env := os.Environ()
	for key, value := range job.Env {
		env = append(env, key+"="+value)
	}
	return env
}

func appendHistory(job string, rec Record) error {
	if err := os.MkdirAll(paths.RunsDir(job), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(paths.HistoryPath(job), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	return err
}

func History(job string) ([]Record, error) {
	data, err := os.ReadFile(paths.HistoryPath(job))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []Record
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, nil
}

func LastRecord(job string) (Record, bool, error) {
	records, err := History(job)
	if err != nil {
		return Record{}, false, err
	}
	if len(records) == 0 {
		return Record{}, false, nil
	}
	return records[len(records)-1], true, nil
}

func pruneRuns(job string) {
	dir := paths.RunsDir(job)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var logs []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".log") {
			logs = append(logs, entry.Name())
		}
	}
	sort.Strings(logs)
	for _, name := range logs[:max(0, len(logs)-keepRuns)] {
		_ = os.Remove(filepath.Join(dir, name))
	}
	trimHistory(job)
}

func trimHistory(job string) {
	records, err := History(job)
	if err != nil || len(records) <= keepRuns {
		return
	}
	var b strings.Builder
	for _, rec := range records[len(records)-keepRuns:] {
		line, _ := json.Marshal(rec)
		b.Write(line)
		b.WriteByte('\n')
	}
	_ = os.WriteFile(paths.HistoryPath(job), []byte(b.String()), 0o644)
}
