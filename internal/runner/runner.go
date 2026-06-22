package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// Reason qualifies a Run's status when the agent did not produce it: the firing
// was dropped (ReasonOverlap, or ReasonCondition for a clean skip), or the
// condition check itself broke (ReasonCondition with StatusFailure). ReasonNone
// is the zero value, used when the agent ran and produced the status.
type Reason string

const (
	ReasonNone      Reason = ""
	ReasonOverlap   Reason = "overlap"
	ReasonCondition Reason = "condition"
)

type Result struct {
	Status   Status
	Reason   Reason
	Exit     int
	Duration time.Duration
	LogPath  string
}

type Record struct {
	Start     string `json:"start"`
	End       string `json:"end"`
	Status    Status `json:"status"`
	Reason    Reason `json:"reason,omitempty"`
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
		return recordSkipped(job.Name, ReasonOverlap)
	}
	defer releaseLock(lock)

	if result, proceed, err := evalCondition(job, timeout); err != nil || !proceed {
		return result, err
	}

	return runAgent(job, timeout)
}

func runAgent(job config.Job, timeout time.Duration) (Result, error) {
	runsDir := paths.RunsDir(job.Name)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return Result{}, err
	}

	start := time.Now()
	logName := start.Format(timestampLayout) + ".log"
	logFile, err := os.Create(filepath.Join(runsDir, logName))
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = logFile.Close() }()

	exit, status := execAgent(job, timeout, io.MultiWriter(logFile, os.Stdout))
	return finishRun(job.Name, start, status, ReasonNone, exit, logName)
}

func recordSkipped(job string, reason Reason) (Result, error) {
	now := time.Now().Format(time.RFC3339)
	rec := Record{Start: now, End: now, Status: StatusSkipped, Reason: reason}
	if err := appendHistory(job, rec); err != nil {
		return Result{}, err
	}
	trimHistory(job)
	return Result{Status: StatusSkipped, Reason: reason}, nil
}

func finishRun(job string, start time.Time, status Status, reason Reason, exit int, logName string) (Result, error) {
	duration := time.Since(start)
	rec := Record{
		Start:     start.Format(time.RFC3339),
		End:       start.Add(duration).Format(time.RFC3339),
		Status:    status,
		Reason:    reason,
		Exit:      exit,
		DurationS: int(duration.Seconds()),
		Log:       logName,
	}
	if err := appendHistory(job, rec); err != nil {
		return Result{}, err
	}
	pruneRuns(job)
	return Result{
		Status:   status,
		Reason:   reason,
		Exit:     exit,
		Duration: duration,
		LogPath:  filepath.Join(paths.RunsDir(job), logName),
	}, nil
}

func execAgent(job config.Job, timeout time.Duration, out io.Writer) (int, Status) {
	exit, timedOut, err := runCmd(substitutePrompt(job.Agent, job.Prompt), job, timeout, out)
	switch {
	case err == nil:
		return 0, StatusSuccess
	case timedOut:
		return exit, StatusTimeout
	default:
		return exit, StatusFailure
	}
}

// runCmd executes argv with the Job's cwd and env, sending combined output to
// out, bounded by timeout (SIGTERM, then SIGKILL after killGrace). It returns
// the process exit code (-1 if it never produced one), whether the timeout
// fired, and the run error.
func runCmd(argv []string, job config.Job, timeout time.Duration, out io.Writer) (int, bool, error) {
	ctx := context.Background()
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

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
		return 0, false, nil
	case ctx.Err() == context.DeadlineExceeded:
		return -1, true, err
	default:
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), false, err
		}
		return -1, false, err
	}
}

type conditionOutcome int

const (
	conditionProceed conditionOutcome = iota
	conditionSkip
	conditionFail
)

// evalCondition runs the Job's condition (if any) before the agent. proceed is
// true when the agent should run; otherwise the Run has already been recorded
// (a skip, or a failure when the check itself broke) and proceed is false. A
// passing condition prints a marker straight to stdout before the agent
// streams; it never reaches the captured run log, so a stream-json log stays
// byte-for-byte the agent's own output.
func evalCondition(job config.Job, timeout time.Duration) (Result, bool, error) {
	if len(job.Condition) == 0 {
		return Result{}, true, nil
	}

	start := time.Now()
	var buf bytes.Buffer
	exit, outcome := execCondition(job, timeout, &buf)
	switch outcome {
	case conditionSkip:
		result, err := recordSkipped(job.Name, ReasonCondition)
		return result, false, err
	case conditionFail:
		result, err := recordConditionFailure(job.Name, start, exit, buf.Bytes())
		return result, false, err
	default:
		fmt.Printf("condition passed %s\n", job.Name)
		return Result{}, true, nil
	}
}

// execCondition runs the condition command and maps its exit to an outcome,
// mirroring systemd ExecCondition=: 0 proceeds, 1-254 is a clean skip, and 255
// or death by signal/timeout is a failure (the check itself is broken).
func execCondition(job config.Job, timeout time.Duration, out io.Writer) (int, conditionOutcome) {
	exit, timedOut, err := runCmd(job.Condition, job, timeout, out)
	switch {
	case err == nil:
		return 0, conditionProceed
	case timedOut:
		return exit, conditionFail
	case exit >= 1 && exit <= 254:
		return exit, conditionSkip
	default:
		return exit, conditionFail
	}
}

// recordConditionFailure records a Run where the condition check itself broke.
// Unlike a clean skip, its output is worth keeping, so it writes a log file.
func recordConditionFailure(job string, start time.Time, exit int, output []byte) (Result, error) {
	runsDir := paths.RunsDir(job)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return Result{}, err
	}
	logName := start.Format(timestampLayout) + ".log"
	if err := os.WriteFile(filepath.Join(runsDir, logName), output, 0o644); err != nil {
		return Result{}, err
	}
	return finishRun(job, start, StatusFailure, ReasonCondition, exit, logName)
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
	if err != nil {
		return
	}
	kept := retainHistory(records)
	if len(kept) == len(records) {
		return
	}
	var b strings.Builder
	for _, rec := range kept {
		line, _ := json.Marshal(rec)
		b.Write(line)
		b.WriteByte('\n')
	}
	_ = os.WriteFile(paths.HistoryPath(job), []byte(b.String()), 0o644)
}

// retainHistory keeps the last keepRuns real Runs and, independently, the last
// keepRuns skipped Runs, in chronological order. Independent caps mean no volume
// of skips can ever evict a real Run from the history.
func retainHistory(records []Record) []Record {
	totalReal, totalSkip := 0, 0
	for _, rec := range records {
		if rec.Status == StatusSkipped {
			totalSkip++
		} else {
			totalReal++
		}
	}
	dropReal := max(0, totalReal-keepRuns)
	dropSkip := max(0, totalSkip-keepRuns)

	seenReal, seenSkip := 0, 0
	kept := make([]Record, 0, len(records))
	for _, rec := range records {
		if rec.Status == StatusSkipped {
			seenSkip++
			if seenSkip <= dropSkip {
				continue
			}
		} else {
			seenReal++
			if seenReal <= dropReal {
				continue
			}
		}
		kept = append(kept, rec)
	}
	return kept
}
