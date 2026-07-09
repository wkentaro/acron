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
	"strings"
	"syscall"
	"time"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
)

// LogTimestampLayout is shared so a log filename can be parsed back into its
// time with the exact layout used to format it.
const LogTimestampLayout = "2006-01-02T15-04-05"

// LogExt is the other half of the log-filename contract: paired with
// LogTimestampLayout it is what writers create and readers parse back.
const LogExt = ".log"

func logFileName(start time.Time) string {
	return start.Format(LogTimestampLayout) + LogExt
}

const (
	killGrace = 10 * time.Second
	keepRuns  = 50
)

type Status string

const (
	StatusSuccess     Status = "success"
	StatusFailure     Status = "failure"
	StatusTimeout     Status = "timeout"
	StatusSkipped     Status = "skipped"
	StatusInterrupted Status = "interrupted"
)

// Reason qualifies a Run's status when the agent did not produce it: the firing
// was dropped (ReasonOverlap, or ReasonCondition for a clean skip), or the
// condition check itself broke or was interrupted (ReasonCondition with
// StatusFailure or StatusInterrupted). ReasonNone is the zero value, used when
// the agent ran — whether it produced the status or an interrupt aborted it.
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
	Command  []string // resolved agent argv, nil when the agent never ran
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

// Run executes the Job's optional condition then its agent, bounded by the
// Job's timeout. ctx carries the caller's interrupt (SIGINT/SIGTERM in the
// `acron run` path): cancelling it records the in-flight Run as interrupted and
// releases the lock, distinct from a deliberate skip or a failure.
func Run(ctx context.Context, job config.Job) (Result, error) {
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

	if result, proceed, err := evalCondition(ctx, job, timeout); err != nil || !proceed {
		return result, err
	}

	return runAgent(ctx, job, timeout, lock)
}

func ensureRunsDir(job string) (string, error) {
	dir := paths.RunsDir(job)
	return dir, os.MkdirAll(dir, 0o755)
}

func runAgent(ctx context.Context, job config.Job, timeout time.Duration, lock *os.File) (Result, error) {
	runsDir, err := ensureRunsDir(job.Name)
	if err != nil {
		return Result{}, err
	}

	start := time.Now()
	logName := logFileName(start)
	// Stamp the live-log name before the file exists on disk, so it is never a
	// deletion candidate that a concurrent overlap-skip's prune cannot see as
	// live. The follower tolerates this gap (the stamp may briefly name a file
	// not yet created); pruneRuns only ever deletes files already on disk.
	recordLiveLog(lock, logName)
	logFile, err := os.Create(filepath.Join(runsDir, logName))
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = logFile.Close() }()

	argv := substitutePrompt(job.Agent, job.Prompt)
	exit, status := execAgent(ctx, argv, job, timeout, io.MultiWriter(logFile, os.Stdout))
	return finishRun(job.Name, start, status, ReasonNone, exit, logName, argv)
}

// appendAndPrune appends rec to the Job's history, then prunes stale run logs.
// Prune, not just trim: a flood of logless skips can evict an older
// condition-skip record that did carry a log, and pruneRuns deletes that
// now-unreferenced log file. trimHistory alone would orphan it on disk.
func appendAndPrune(job string, rec Record) error {
	if err := appendHistory(job, rec); err != nil {
		return err
	}
	pruneRuns(job)
	return nil
}

func recordSkipped(job string, reason Reason) (Result, error) {
	now := time.Now().Format(time.RFC3339)
	rec := Record{Start: now, End: now, Status: StatusSkipped, Reason: reason}
	if err := appendAndPrune(job, rec); err != nil {
		return Result{}, err
	}
	return Result{Status: StatusSkipped, Reason: reason}, nil
}

func finishRun(job string, start time.Time, status Status, reason Reason, exit int, logName string, argv []string) (Result, error) {
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
	if err := appendAndPrune(job, rec); err != nil {
		return Result{}, err
	}
	return Result{
		Status:   status,
		Reason:   reason,
		Exit:     exit,
		Duration: duration,
		LogPath:  filepath.Join(paths.RunsDir(job), logName),
		Command:  argv,
	}, nil
}

func execAgent(ctx context.Context, argv []string, job config.Job, timeout time.Duration, out io.Writer) (int, Status) {
	exit, outcome, err := runCmd(ctx, argv, job, timeout, out, out)
	switch {
	case outcome == cmdInterrupted:
		return exit, StatusInterrupted
	case outcome == cmdTimedOut:
		return exit, StatusTimeout
	case err == nil:
		return 0, StatusSuccess
	default:
		return exit, StatusFailure
	}
}

// cmdOutcome names why a runCmd child stopped, beyond its exit code: it ran to
// completion (cmdDone, success or a non-zero exit), the timeout fired
// (cmdTimedOut), or the caller cancelled (cmdInterrupted). The three are
// mutually exclusive, so one value replaces a pair of parallel booleans.
type cmdOutcome int

const (
	cmdDone cmdOutcome = iota
	cmdTimedOut
	cmdInterrupted
)

// runCmd executes argv with the Job's cwd and env, sending stdout and stderr to
// the given writers, bounded by timeout (SIGTERM, then SIGKILL after killGrace).
// Passing one writer for both (as the agent does) interleaves the streams as a
// terminal would; passing separate writers (as the condition check does) keeps
// them apart so stderr can be told from stdout. Cancelling ctx interrupts the
// run the same way. It returns the process exit code (-1 if it never produced
// one, or when the run was externally terminated), the outcome, and the run
// error. A shell-wrapped child interrupted by Ctrl-C exits 128+SIGINT, which is
// indistinguishable from a deliberate exit code, so the interrupt is detected
// from ctx rather than guessed from the exit.
func runCmd(ctx context.Context, argv []string, job config.Job, timeout time.Duration, stdout, stderr io.Writer) (exit int, outcome cmdOutcome, err error) {
	timeoutCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		timeoutCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(timeoutCtx, argv[0], argv[1:]...)
	cmd.Dir = paths.ExpandHome(job.Cwd)
	cmd.Env = jobEnv(job)
	cmd.Stdin = nil // nil stdin connects the child to /dev/null
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = killGrace // SIGKILL if it ignores SIGTERM within the grace period

	// A caller interrupt is detected on the caller's ctx (cancelled), not the
	// timeout-wrapped one (a child reaped after the deadline reports
	// DeadlineExceeded even when the real cause was the cancel). It is checked
	// before the exit code, so an agent that traps the signal and exits cleanly
	// is still recorded as interrupted, and it takes priority over a simultaneous
	// deadline.
	err = cmd.Run()
	switch {
	case ctx.Err() == context.Canceled:
		return -1, cmdInterrupted, err
	case err == nil:
		return 0, cmdDone, nil
	case timeoutCtx.Err() == context.DeadlineExceeded:
		return -1, cmdTimedOut, err
	default:
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), cmdDone, err
		}
		return -1, cmdDone, err
	}
}

type conditionOutcome int

const (
	conditionProceed conditionOutcome = iota
	conditionSkip
	conditionFail
	conditionInterrupt
)

// evalCondition runs the Job's condition (if any) before the agent. proceed is
// true when the agent should run; otherwise the Run has already been recorded
// (a skip, or a failure when the check itself broke) and proceed is false. A
// passing condition prints a marker straight to stdout before the agent
// streams; it never reaches the captured run log, so a stream-json log stays
// byte-for-byte the agent's own output.
func evalCondition(ctx context.Context, job config.Job, timeout time.Duration) (Result, bool, error) {
	if len(job.Condition) == 0 {
		return Result{}, true, nil
	}

	start := time.Now()
	var stdout, stderr bytes.Buffer
	exit, outcome := execCondition(ctx, job, timeout, &stdout, &stderr)
	switch outcome {
	case conditionSkip:
		// A well-behaved gate is a quiet predicate (test, grep -q) or prints its
		// result to stdout (jq -e emits `false` on the negative case), so stdout
		// alone is an ordinary skip and records nothing extra, like an overlap
		// skip. Output on stderr is the tell of broken tooling (command-not-found
		// exit 127, an unauthenticated gh, a `test: integer expected` exit 2), so
		// preserve it to a log surfaced via `acron logs`.
		if stderr.Len() == 0 {
			result, err := recordSkipped(job.Name, ReasonCondition)
			return result, false, err
		}
		result, err := recordConditionOutcome(job.Name, start, StatusSkipped, exit, combinedOutput(&stdout, &stderr))
		return result, false, err
	case conditionFail:
		result, err := recordConditionOutcome(job.Name, start, StatusFailure, exit, combinedOutput(&stdout, &stderr))
		return result, false, err
	case conditionInterrupt:
		result, err := recordConditionOutcome(job.Name, start, StatusInterrupted, exit, combinedOutput(&stdout, &stderr))
		return result, false, err
	default:
		fmt.Printf("condition passed %s\n", job.Name)
		return Result{}, true, nil
	}
}

// combinedOutput joins a condition's captured streams for its log. The streams
// are buffered separately so stderr can be told from stdout, so the log is
// stdout-then-stderr rather than emission-interleaved; for the short predicates
// a gate runs this is a non-issue, and the streams rarely overlap.
func combinedOutput(stdout, stderr *bytes.Buffer) []byte {
	out := make([]byte, 0, stdout.Len()+stderr.Len())
	out = append(out, stdout.Bytes()...)
	return append(out, stderr.Bytes()...)
}

// execCondition runs the condition command and maps its exit to an outcome,
// mirroring systemd ExecCondition=: 0 proceeds, 1-254 is a clean skip, and 255
// or death by signal/timeout is a failure (the check itself is broken).
func execCondition(ctx context.Context, job config.Job, timeout time.Duration, stdout, stderr io.Writer) (int, conditionOutcome) {
	exit, outcome, err := runCmd(ctx, job.Condition, job, timeout, stdout, stderr)
	switch {
	case outcome == cmdInterrupted:
		return exit, conditionInterrupt
	case outcome == cmdTimedOut:
		return exit, conditionFail
	case err == nil:
		return 0, conditionProceed
	case exit >= 1 && exit <= 254:
		return exit, conditionSkip
	default:
		return exit, conditionFail
	}
}

// recordConditionOutcome records a Run produced by the condition check rather
// than the agent, preserving the condition's captured output in a log file so
// the reason (a broken check, an interrupt mid-check, or a skip whose tooling
// misfired) is discoverable via `acron logs`.
func recordConditionOutcome(job string, start time.Time, status Status, exit int, output []byte) (Result, error) {
	runsDir, err := ensureRunsDir(job)
	if err != nil {
		return Result{}, err
	}
	logName := logFileName(start)
	if err := os.WriteFile(filepath.Join(runsDir, logName), output, 0o644); err != nil {
		return Result{}, err
	}
	return finishRun(job, start, status, ReasonCondition, exit, logName, nil)
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
	// Clear any name a previous Run left if it crashed without releasing
	// (releaseLock clears it on the normal path), so an empty held lock always
	// means this Run's Condition check, never a stale name.
	_ = file.Truncate(0)
	return file, true, nil
}

// recordLiveLog stamps the in-flight Run's log file name into the held lock
// file, so a follower can read which log the agent is currently streaming to.
// The lock file is empty during the Condition check (acquireLock cleared it);
// awaitLiveLog treats that empty-while-held state as "agent not started yet".
// Log names are fixed-length, so this single WriteAt overwrites the cleared
// file with the full name in one operation, never a partial name. Best-effort:
// the stamp is advisory, so a write error must not fail the Run it guards.
func recordLiveLog(lock *os.File, logName string) {
	_, _ = lock.WriteAt([]byte(logName), 0)
}

// liveLogName reads the in-flight Run's log file name from the Job's lock file,
// or "" if no name is stamped (no Run, or a Run still in its Condition check).
func liveLogName(job string) string {
	data, err := os.ReadFile(paths.LockPath(job))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func releaseLock(file *os.File) {
	// Clear the stamped log name while still holding the lock, so the gap
	// between the next Run's lock-acquire and its Condition check never exposes
	// this finished Run's name to a follower.
	_ = file.Truncate(0)
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

// IsRunning reports whether a Run currently holds the Job's lock. It is a
// best-effort, non-destructive probe of the same flock acquireLock takes: a
// successful try-lock (released at once) means no Run is in flight, EWOULDBLOCK
// means one is. The result is advisory; a firing can still begin the instant
// after it returns. It opens without O_CREATE so probing a Job that has never
// run reports false rather than littering a lock file.
func IsRunning(job string) bool {
	file, err := os.OpenFile(paths.LockPath(job), os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err == syscall.EWOULDBLOCK
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

// InFlight reports whether a Run holds the Job's lock and, while the agent is
// streaming, the name of the log file it is writing to. The name is empty
// during an earlier Condition check, before the agent's log exists.
func InFlight(job string) (logName string, running bool) {
	if !IsRunning(job) {
		return "", false
	}
	return liveLogName(job), true
}

// RunningSince reports whether a Run is in flight for the Job and, when known,
// when it started. The start time is parsed from the in-flight Run's log file
// name; it is unknown (zero) during an earlier Condition check, before the
// agent's log exists. The name is stamped from a local time.Now(), so it parses
// back in time.Local to recover the true instant (not just the right digits).
func RunningSince(job string) (time.Time, bool) {
	logName, running := InFlight(job)
	if !running {
		return time.Time{}, false
	}
	if logName == "" {
		return time.Time{}, true
	}
	start, err := time.ParseInLocation(LogTimestampLayout, strings.TrimSuffix(logName, LogExt), time.Local)
	if err != nil {
		return time.Time{}, true
	}
	return start, true
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
	if _, err := ensureRunsDir(job); err != nil {
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

// pruneRuns trims the history to its retention caps, then deletes every log file
// no surviving record still references. Tying the on-disk logs to the kept
// records (rather than to a flat file count) lets retainHistory's independent
// real/skip caps stay authoritative: a flood of skip logs can never evict a real
// Run's log from disk while its record still lives in history. It deletes off the
// in-memory kept set trimHistory returns, so a failed history rewrite never
// causes a still-referenced log to be pruned; an empty kept set (the history is
// gone or unwritten) bails rather than deleting every log it cannot account for.
// An in-flight Run's live log, on disk but not yet in any record, is kept too.
func pruneRuns(job string) {
	kept, ok := trimHistory(job)
	if !ok || len(kept) == 0 {
		return
	}
	referenced := make(map[string]bool, len(kept)+1)
	for _, rec := range kept {
		if rec.Log != "" {
			referenced[rec.Log] = true
		}
	}
	// An in-flight Run stamps its live-log name into the lock before it creates the
	// file, and well before finishRun writes the record, so a concurrent
	// overlap-skip's prune would delete the log the running agent is still
	// streaming to. The lock file names that live log; keep it referenced until the
	// record lands. The stamp can briefly name a file not yet on disk, which is
	// harmless: prune only deletes files it actually finds in the directory.
	if live := liveLogName(job); live != "" {
		referenced[live] = true
	}
	dir := paths.RunsDir(job)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, LogExt) && !referenced[name] {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// trimHistory caps the on-disk history to its retention limits and returns the
// records it kept. ok is false only when the history could not be read, so a
// caller (pruneRuns) can tell an empty history from an unreadable one and avoid
// pruning logs it cannot account for.
func trimHistory(job string) (kept []Record, ok bool) {
	records, err := History(job)
	if err != nil {
		return nil, false
	}
	kept = retainHistory(records)
	if len(kept) == len(records) {
		return kept, true
	}
	var b strings.Builder
	for _, rec := range kept {
		line, _ := json.Marshal(rec)
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(paths.HistoryPath(job), []byte(b.String()), 0o644); err != nil {
		return nil, false
	}
	return kept, true
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
