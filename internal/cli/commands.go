package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/runner"
	"github.com/wkentaro/acron/internal/scheduler"
)

func loadConfig() (*config.Config, error) {
	return loadAndValidate(config.DefaultPath())
}

func completeJobNames(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	candidates := make([]string, 0, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		candidate := job.Name
		if hint := completionHint(job); hint != "" {
			candidate = cobra.CompletionWithDesc(candidate, hint)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, cobra.ShellCompDirectiveNoFileComp
}

func completionHint(job config.Job) string {
	hint := promptHint(job.Prompt)
	if hint == "" {
		return ""
	}
	// Truncate the prompt before appending the cwd basename so the basename,
	// which disambiguates jobs that share a prompt, always survives.
	hint = truncateHint(hint)
	if cwd := cwdHint(job.Cwd); cwd != "" {
		hint += " — " + cwd
	}
	return hint
}

// promptHint is selection help, not a second authored description field.
func promptHint(prompt string) string {
	for prompt != "" {
		line, rest, _ := strings.Cut(prompt, "\n")
		if hint := strings.Join(strings.Fields(line), " "); hint != "" {
			return hint
		}
		prompt = rest
	}
	return ""
}

func cwdHint(cwd string) string {
	base := filepath.Base(paths.ExpandHome(cwd))
	if base == "." || base == "/" {
		return ""
	}
	return base
}

func truncateHint(hint string) string {
	const limit = 56
	if len(hint) <= limit { // fast path: byte length is an upper bound on rune count
		return hint
	}
	runes := []rune(hint)
	if len(runes) <= limit {
		return hint
	}
	return string(runes[:limit-1]) + "…"
}

func requireJob(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if _, ok := cfg.FindJob(name); !ok {
		return fmt.Errorf("no job named %q", name)
	}
	return nil
}

func loadAndValidate(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Reconcile OS scheduler units to the config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			return runApply(dryRun)
		},
	}
	cmd.Flags().Bool("dry-run", false, "Print the plan without changing anything")
	return cmd
}

func newDestroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Remove all acron-owned units from this machine",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runDestroy()
		},
	}
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "run <job>",
		Short:             "Run a job now (the entry the scheduler invokes)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeJobNames,
		RunE: func(_ *cobra.Command, args []string) error {
			return runJob(args[0])
		},
	}
}

func newTriggerCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "trigger <job>",
		Short:             "Fire a job now, out of schedule, in the background",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeJobNames,
		RunE: func(_ *cobra.Command, args []string) error {
			return runTrigger(args[0])
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show each job's apply state and last run",
		Long: `Show each Job's Apply state and latest Run, one row per Job.

Rows are the union of Config Jobs and any acron-owned units installed on this
machine, so an orphaned unit (still installed for a Job no longer in the Config)
appears even though it has no Config entry. Each Job is reported on two
independent axes: its Apply state (computed from the same comparison 'acron
apply' performs) and its Run status (the last record in the Run history).

Columns:

  JOB     The Job's name.

  APPLY   The Job's Apply state:
            applied    units match the Config and the timer is active; 'apply'
                       would be a no-op
            drifted    'apply' would rewrite or restart the units; run 'acron apply'
            unapplied  declared in the Config but not yet installed; run 'acron apply'
            orphaned   units installed for a Job no longer in the Config; 'apply'
                       auto-prunes these
            disabled   the Job sets enabled = false, so 'apply' keeps it uninstalled

  STATUS  The latest Run's outcome:
            success                     the agent exited zero
            failure                     the agent exited non-zero
            timeout                     the Run exceeded its timeout
            interrupted                 the operator aborted it before a verdict
            skipped (overlap)           the previous Run still held the lock, so
                                        this firing was dropped
            skipped (condition)         the Condition returned non-zero, a clean
                                        negative; no agent ran, shown dimmed
            skipped (condition, output) the Condition check also wrote to stderr,
                                        which usually means the check is broken;
                                        shown in yellow, run 'acron logs <job>'
                                        to inspect
            condition                   the Condition check is running; a live
                                        state, never stored, shown in yellow;
                                        LAST and PASSED stay blank until the
                                        agent starts
            running                     the agent is running now; a live state,
                                        never stored, shown in yellow; LAST and
                                        PASSED track the agent start
            never run                   no Run has been recorded yet

          A Condition check that breaks rather than returning a clean negative
          surfaces under its own outcome, as failure (condition) or interrupted
          (condition), not as a skip.

  LAST    Absolute start time of the latest Run (local time). This exact string
          is accepted by 'acron logs <job> "<LAST>"' as a run selector.

  PASSED  Elapsed since LAST (e.g. 1h 35min ago); for a running Job, elapsed
          since the Run started; blank when LAST is blank.

  NEXT    Next scheduled fire time; shown only for applied Jobs; — (not
          applicable) for every other Apply state.

  LEFT    Time remaining until NEXT (e.g. 12min 30s); — when NEXT is —.`,
		Example: `
acron status
`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runStatus()
		},
	}
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "show <job>",
		Short:             "Show a job's generated unit and whether it matches what is installed",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeJobNames,
		RunE: func(_ *cobra.Command, args []string) error {
			return runShow(args[0])
		},
	}
}

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <job> [run]",
		Short: "Show a job's captured output",
		Long: `Show a Job's captured agent output for one Run.

Run selector:

  (none) / latest  The newest Run that produced output. A Run with no captured
                   output is passed over for the newest one that has it.

  <timestamp>      A specific Run, named by the WHEN value 'acron history'
                   prints for it. The exact string history shows round-trips:
                   'acron logs <job> "2026-06-22 02:00:00"' selects that Run.
                   The timestamp is local time, in YYYY-MM-DD HH:MM:SS format.

Output split:

  Before the log body, logs prints one header line to stderr:

    <job>  <WHEN>  <STATUS>  in <DURATION>

  e.g. 'nightly-triage  2026-06-22 02:00:00  success  in 13min 48s'. Selecting the
  live Run by its timestamp shows STATUS 'running' and a 'running for <elapsed>'
  tail instead, since it has no final duration yet.

  The header goes to stderr; the log body is the sole stdout payload. So
  'acron logs <job> | grep …' and 'acron logs <job> > run.log' see the clean
  log without the metadata line.

--follow, -f:

  Attaches to the Run currently in progress and streams its log from the start
  of the Run until the Run finishes.

  It accepts no selector or 'latest' (either one attaches to the live Run) and
  rejects an explicit timestamp (a finished Run never grows, so following one is
  meaningless). It requires a live Run: with nothing in flight it errors with
  'no run in progress for "<job>"'.

  If the lock is held but no agent log exists yet (the Condition check is still
  running), it prints 'waiting for condition...' to stderr once and waits for
  the agent to start.

  On completion it prints a one-line footer to stderr:

    run success in 4min 12s
    run failure (exit 1) in 2min 3s
    run skipped (condition)

  An '(output)' note on a skipped footer (e.g. 'run skipped (condition,
  output)') means the Condition check wrote to stderr, which usually means the
  check is broken; read the log to see what it printed.`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeJobNames,
		Example: `
acron logs nightly-triage                       # Newest run (same as "latest")
acron logs nightly-triage latest                # Newest run explicitly
acron logs nightly-triage "2026-06-22 02:00:00" # A specific run by its displayed timestamp
acron logs nightly-triage --follow              # Stream the run in progress until it finishes
`,
		RunE: func(_ *cobra.Command, args []string) error {
			selector := ""
			if len(args) == 2 {
				selector = args[1]
			}
			if follow {
				return runFollow(args[0], selector)
			}
			return runLogs(args[0], selector)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream the run in progress until it finishes")
	return cmd
}

func newHistoryCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "history [job]",
		Short: "List past runs, newest first",
		Long: `List every Job's Runs as a single time-ordered table, newest first, one row
per Run.

Defaults to the 20 most recent Runs across all Jobs; --limit N sets the count
and --limit 0 removes the cap. The table is interleaved, not grouped by Job:
'acron history <job>' is the same table filtered to one Job, with the JOB column
kept, so the filtered view has the identical shape to the unfiltered one: a
filter, not a separate view. A skipped Run appears like any other; it is a real
outcome, and hiding it would let 'history' and 'status' disagree about the
latest Run.

Columns:

  JOB       The Job's name; kept even when filtered to one Job, so the table
            shape is identical whether filtered or not.

  WHEN      Absolute start time of the Run (2006-01-02 15:04:05, local time).
            This exact string is the 'acron logs' run selector: 'acron logs
            <job> "<WHEN>"' opens that Run's output, and the string printed here
            is the string 'logs' accepts, so copy-paste round-trips.

  PASSED    Elapsed since the Run started (e.g. 2h 15min ago); the same format
            'acron status' carries.

  STATUS    The Run's outcome:
              success                     the agent exited zero
              failure                     the agent exited non-zero
              timeout                     the Run exceeded its timeout
              skipped (overlap)           the previous Run still held the lock,
                                          so this firing was dropped
              skipped (condition)         the Condition returned a clean
                                          negative; no agent ran
              skipped (condition, output) the Condition also wrote to stderr,
                                          which usually means the check is broken
              interrupted                 the operator aborted it before a
                                          verdict
              running                     the agent is running now
            'running' is a live state synthesized from the held lock, never a
            stored value; it sorts to the top of the table. It appears only once
            the agent has started; a Run still in its Condition check has no
            WHEN yet and does not appear.

            A Condition check that breaks rather than returning a clean negative
            surfaces under its own outcome, as failure (condition) or
            interrupted (condition), not as a skip.

  DURATION  How long the agent ran; — (no final duration) for a skipped Run,
            whose agent never ran, and for the in-flight 'running' row.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeJobNames,
		Example: `
acron history                 # 20 most recent runs across all jobs
acron history nightly-triage  # 20 most recent runs of one job
acron history --limit 100     # Show more
acron history --limit 0       # Show all
`,
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runHistory(name, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "Number of most recent runs to show (0 for all)")
	return cmd
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or edit the config",
	}
	cmd.AddCommand(newConfigShowCmd(), newConfigEditCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the config to stdout",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runConfigShow()
		},
	}
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the config in $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runEdit()
		},
	}
}

func runApply(dryRun bool) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	plan, err := scheduler.Apply(cfg, dryRun)
	if err != nil {
		return err
	}
	header := "Applied:"
	if dryRun {
		header = "Would apply:"
	}
	fmt.Print(renderPlan(plan, header, dryRun))
	return nil
}

func runDestroy() error {
	plan, err := scheduler.Destroy()
	if err != nil {
		return err
	}
	fmt.Print(renderPlan(plan, "Destroyed:", false))
	return nil
}

func runJob(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	job, ok := cfg.FindJob(name)
	if !ok {
		return fmt.Errorf("no job named %q", name)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result, err := runner.Run(ctx, job)
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("%s  %s", renderStatus(result.Status, result.Reason, result.LogPath), name)
	if result.Exit >= 0 {
		summary += fmt.Sprintf("  exit %d", result.Exit)
	}
	fmt.Printf("%s  %s\n", summary, result.Duration.Round(time.Second))
	if len(result.Command) > 0 {
		fmt.Println(commentStyle.Render(renderCommand(result.Command)))
	}
	if result.LogPath != "" {
		fmt.Println(commentStyle.Render(result.LogPath))
	}
	if result.Status == runner.StatusInterrupted {
		return errInterrupted
	}
	return nil
}

func runTrigger(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	states, err := scheduler.ApplyStates(cfg)
	if err != nil {
		return err
	}
	state := scheduler.ApplyState("")
	found := false
	for _, st := range states {
		if st.Name == name {
			state, found = st.State, true
			break
		}
	}
	if !found {
		return fmt.Errorf("no job named %q", name)
	}
	if state != scheduler.StateApplied {
		switch state {
		case scheduler.StateDisabled:
			return fmt.Errorf("job %q is disabled; enable it and run `acron apply`", name)
		case scheduler.StateDrifted:
			return fmt.Errorf("job %q has drifted; run `acron apply` before triggering", name)
		case scheduler.StateOrphaned:
			return fmt.Errorf("job %q is orphaned: a unit is installed but it is not in the config", name)
		default:
			return fmt.Errorf("job %q is not applied; run `acron apply` first", name)
		}
	}
	if phase, _, ok := liveRunPhase(name); ok {
		fmt.Printf("%s  %s  %s\n", runningStyle.Render(phase), name,
			commentStyle.Render("a run is already in progress; not triggered"))
		return nil
	}
	if err := scheduler.Trigger(name); err != nil {
		return err
	}
	fmt.Printf("%s  %s\n", addStyle.Render("triggered"), name)
	fmt.Println(commentStyle.Render("acron logs " + name))
	return nil
}

func runStatus() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	states, err := scheduler.ApplyStates(cfg)
	if err != nil {
		return err
	}
	if len(states) == 0 {
		printNoJobs()
		return nil
	}
	jobs := make(map[string]config.Job, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		jobs[job.Name] = job
	}
	now := time.Now()
	t := statusTable()
	for _, st := range states {
		status, last, passed, err := renderLastRun(st.Name, now)
		if err != nil {
			return err
		}
		next, left := renderNext(jobs[st.Name], st.State, now)
		t.Row(cmdStyle.Render(st.Name), renderApplyState(st.State), status, last, passed, next, left)
	}
	fmt.Print(renderTable(t))
	return nil
}

func runShow(name string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	units, err := scheduler.Show(cfg, name)
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s\n", cmdStyle.Render(units.Name), renderApplyState(units.State))
	for _, unit := range units.Units {
		fmt.Println()
		fmt.Println(commentStyle.Render("# " + unit.Name))
		fmt.Print(renderUnit(unit))
	}
	return nil
}

// renderUnit shows a unit's full content. On drift it shows the whole unit with
// the installed-vs-desired delta marked inline (-/+), since `show` is about the
// unit's content, not just the change. When only one side exists (unapplied has
// no installed, orphaned has no desired), it shows that side plainly.
func renderUnit(unit scheduler.UnitFile) string {
	if unit.Desired != "" && unit.Installed != "" && unit.Desired != unit.Installed {
		return renderUnitFull(unit.Name, unit.Installed, unit.Desired)
	}
	if unit.Desired != "" {
		return unit.Desired
	}
	return unit.Installed
}

// renderNext shows the schedule's next fire only for applied jobs, where the
// config-computed time equals what the installed unit will actually do. Drifted,
// orphaned, unapplied, and disabled jobs would each show a time that does not
// match reality, so they render a placeholder instead. Orphaned jobs have no
// config entry, so the caller passes a zero Job; the non-applied guard returns
// the placeholder before its empty schedule is ever read. The LEFT cell (time
// until that fire, no suffix) is derived from the same next-fire time so the two
// always agree, and mirrors NEXT's placeholder when there is no computable fire.
func renderNext(job config.Job, state scheduler.ApplyState, now time.Time) (next, left string) {
	placeholder := commentStyle.Render("—")
	if state != scheduler.StateApplied {
		return placeholder, placeholder
	}
	fire, err := job.NextFire(now)
	if err != nil || fire.IsZero() {
		return placeholder, placeholder
	}
	return commentStyle.Render(fire.Local().Format(displayTimeFormat)),
		commentStyle.Render(formatDuration(fire.Sub(now)))
}

func renderTable(t *table.Table) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(t.Render(), "\n"), "\n") {
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
	}
	return b.String()
}

// newTable builds the borderless, left-aligned table shared by `status` and
// `history`: dim headers, every column but the last padded two spaces right.
func newTable(headers ...string) *table.Table {
	styled := make([]string, len(headers))
	for i, h := range headers {
		styled[i] = commentStyle.Render(h)
	}
	return table.New().
		BorderTop(false).BorderBottom(false).BorderLeft(false).
		BorderRight(false).BorderColumn(false).BorderHeader(false).
		Headers(styled...).
		StyleFunc(func(_, col int) lipgloss.Style {
			if col < len(headers)-1 {
				return lipgloss.NewStyle().PaddingRight(2)
			}
			return lipgloss.NewStyle()
		})
}

func statusTable() *table.Table {
	return newTable("JOB", "APPLY", "STATUS", "LAST", "PASSED", "NEXT", "LEFT")
}

func historyTable() *table.Table {
	return newTable("JOB", "WHEN", "PASSED", "STATUS", "DURATION")
}

func renderApplyState(state scheduler.ApplyState) string {
	return applyStateStyle(state).Render(string(state))
}

func applyStateStyle(state scheduler.ApplyState) lipgloss.Style {
	switch state {
	case scheduler.StateApplied:
		return addStyle
	case scheduler.StateDrifted, scheduler.StateOrphaned:
		return removeStyle
	default:
		return commentStyle
	}
}

// renderLastRun produces the STATUS, LAST, and PASSED cells for a job. On the
// normal path PASSED (elapsed since the run start, with an "ago" suffix) is
// derived from the same parsed timestamp as LAST, and is blank when LAST has no
// time to show: a never-run job, or an in-flight run still in its `condition`
// phase before the agent start is known. A `running` job's PASSED doubles as
// how long the agent has been running.
func renderLastRun(job string, now time.Time) (status, last, passed string, err error) {
	if phase, since, ok := liveRunPhase(job); ok {
		status = runningStyle.Render(phase)
		if phase == livePhaseRunning {
			last = commentStyle.Render(since.Local().Format(displayTimeFormat))
			passed = renderPassed(now.Sub(since))
		}
		return status, last, passed, nil
	}
	rec, ok, err := runner.LastRecord(job)
	if err != nil {
		return "", "", "", err
	}
	if !ok {
		return commentStyle.Render("never run"), "", "", nil
	}
	status = renderStatus(rec.Status, rec.Reason, rec.Log)
	start, parseErr := time.Parse(time.RFC3339, rec.Start)
	if parseErr != nil {
		return status, commentStyle.Render(rec.Start), "", nil
	}
	return status, commentStyle.Render(start.Local().Format(displayTimeFormat)), renderPassed(now.Sub(start)), nil
}

func renderPassed(d time.Duration) string {
	return commentStyle.Render(formatDuration(d) + " ago")
}

const (
	livePhaseCondition = "condition"
	livePhaseRunning   = "running"
)

// liveRunPhase reports the current live phase for a Job that still holds its
// run lock. The phase is `condition` until the agent log exists, at which point
// the phase becomes `running` and since is the agent start time recovered from
// that log name.
func liveRunPhase(job string) (phase string, since time.Time, ok bool) {
	since, ok = runner.RunningSince(job)
	if !ok {
		return "", time.Time{}, false
	}
	if since.IsZero() {
		return livePhaseCondition, time.Time{}, true
	}
	return livePhaseRunning, since, true
}

func runLogs(job, selector string) error {
	if err := requireJob(job); err != nil {
		return err
	}
	rec, running, err := resolveLog(job, selector)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, renderLogSummary(job, rec, running, time.Now()))
	return copyLog(job, rec.Log)
}

// renderLogSummary orients the reader with a one-line header before the log
// body: job, the run's displayed timestamp, its status, and how long it ran. It
// goes to stderr (like the --follow footer) so stdout stays the pure log payload.
// An in-flight agent-phase Run has no final duration yet, so it reports how
// long it has been running instead.
func renderLogSummary(job string, rec runner.Record, inFlight bool, now time.Time) string {
	var status, tail string
	if inFlight {
		status = runningStyle.Render(livePhaseRunning)
		start, _ := time.Parse(time.RFC3339, rec.Start)
		tail = "running for " + formatDuration(now.Sub(start))
	} else {
		status = renderStatus(rec.Status, rec.Reason, rec.Log)
		tail = "in " + formatDuration(recDuration(rec))
	}
	return strings.Join([]string{
		cmdStyle.Render(job),
		commentStyle.Render(formatWhen(rec.Start)),
		status,
		commentStyle.Render(tail),
	}, "  ")
}

func copyLog(job, logName string) error {
	f, err := os.Open(filepath.Join(paths.RunsDir(job), logName))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(os.Stdout, f)
	return err
}

const followPollInterval = 200 * time.Millisecond

// runFollow attaches to the Job's in-flight Run and streams its agent transcript
// to stdout until the Run finishes, then prints a one-line status footer to
// stderr. It refuses an explicit run selector (a finished Run never grows) and
// errors when no Run is in flight to attach to.
func runFollow(job, selector string) error {
	if selector != "" && selector != "latest" {
		return fmt.Errorf("--follow attaches to the in-flight run; it cannot be combined with a run selector")
	}
	if err := requireJob(job); err != nil {
		return err
	}
	if !runner.IsRunning(job) {
		return fmt.Errorf("no run in progress for %q", job)
	}

	logName, ok := awaitLiveLog(job)
	if !ok {
		return followFinished(job)
	}
	return streamLiveLog(job, logName)
}

// awaitLiveLog waits for the in-flight Run's agent log to appear, polling past
// the Condition check during which no log exists yet. It returns false if the
// Run ends before any agent log streams (a skip, or a Run that finished as we
// attached).
func awaitLiveLog(job string) (string, bool) {
	notified := false
	for {
		logName, running := runner.InFlight(job)
		if !running {
			return "", false
		}
		if logName != "" {
			return logName, true
		}
		if !notified {
			fmt.Fprintln(os.Stderr, "waiting for condition...")
			notified = true
		}
		time.Sleep(followPollInterval)
	}
}

// openLiveLog opens the in-flight Run's agent log, waiting for the file to
// appear. runAgent stamps the live-log name into the lock before it creates the
// file, so awaitLiveLog can hand back a name a beat before the file exists; this
// polls past that gap rather than failing on the transient absence. It returns
// opened=false if the Run ends before the file ever appears (log creation
// failed after the stamp), so the caller can fall back to the recorded result.
func openLiveLog(job, logName string) (*os.File, bool, error) {
	path := filepath.Join(paths.RunsDir(job), logName)
	for {
		file, err := os.Open(path)
		if err == nil {
			return file, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, err
		}
		// The file is not there yet. Keep waiting only while this same Run still
		// holds the lock and stamps this name; if it released the lock or a later
		// Run took over the stamp, the file we were promised will never appear.
		if live, running := runner.InFlight(job); !running || live != logName {
			return nil, false, nil
		}
		time.Sleep(followPollInterval)
	}
}

func streamLiveLog(job, logName string) error {
	f, opened, err := openLiveLog(job, logName)
	if err != nil {
		return err
	}
	if !opened {
		// The Run ended before its agent log file appeared (its creation failed
		// right after the live-log stamp), so fall back to the recorded result.
		return followFinished(job)
	}
	defer func() { _ = f.Close() }()

	for {
		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}
		if !runner.IsRunning(job) {
			// The lock is released only after the runner has written the
			// final bytes and recorded the Run, so one last copy drains the
			// tail the loop's previous copy could not yet see.
			if _, err := io.Copy(os.Stdout, f); err != nil {
				return err
			}
			break
		}
		time.Sleep(followPollInterval)
	}
	return printFollowFooter(job, logName)
}

// followFinished handles a Run that ended before any agent log streamed: it
// prints that Run's complete log if it produced one, then the status footer,
// rather than erroring after we already reported the Run as live.
func followFinished(job string) error {
	rec, ok, err := runner.LastRecord(job)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("run for %q ended without a recorded result", job)
	}
	if rec.Log != "" {
		if err := copyLog(job, rec.Log); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stderr, followFooter(rec))
	return nil
}

// printFollowFooter prints the status footer for the Run identified by logName,
// found by its log name rather than by "last record" so a Run that starts and
// finishes between the end of the stream and this lookup cannot shadow it.
func printFollowFooter(job, logName string) error {
	records, err := runner.History(job)
	if err != nil {
		return err
	}
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Log == logName {
			fmt.Fprintln(os.Stderr, followFooter(records[i]))
			return nil
		}
	}
	fmt.Fprintln(os.Stderr, "run finished (no recorded result)")
	return nil
}

func followFooter(rec runner.Record) string {
	var notes []string
	if rec.Reason != "" {
		notes = append(notes, string(rec.Reason))
	}
	if rec.Status == runner.StatusSkipped {
		// Mirror renderStatus: a skip's recorded exit is the condition's, not the
		// agent's (which never ran), so a log is the honest "this skip is suspect"
		// signal, the same "output" annotation the history table and status cell show.
		if rec.Log != "" {
			notes = append(notes, "output")
		}
	} else if rec.Exit > 0 {
		notes = append(notes, fmt.Sprintf("exit %d", rec.Exit))
	}
	msg := "run " + string(rec.Status)
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, ", ") + ")"
	}
	if rec.Status == runner.StatusSkipped {
		return msg
	}
	return msg + " in " + formatDuration(recDuration(rec))
}

// runHistory renders past Runs as one interleaved, newest-first table. With no
// job it spans every job (the JOB column repeats); with a job it filters to that
// one (the column stays, so the filtered view is the same table with rows
// removed). limit caps the rows to the most recent N across the selection; 0
// shows all. Skipped Runs appear like any other outcome. An in-flight Run shows
// as a `running` row at the top with no duration yet once the agent has started;
// it is omitted while still in `condition`, before a start time exists.
func runHistory(name string, limit int) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	var jobs []config.Job
	if name == "" {
		jobs = cfg.Jobs
	} else {
		job, ok := cfg.FindJob(name)
		if !ok {
			return fmt.Errorf("no job named %q", name)
		}
		jobs = []config.Job{job}
	}
	if len(jobs) == 0 {
		printNoJobs()
		return nil
	}

	type jobRun struct {
		job     string
		rec     runner.Record
		start   time.Time
		running bool
	}
	var runs []jobRun
	for _, job := range jobs {
		records, err := runner.History(job.Name)
		if err != nil {
			return err
		}
		for _, rec := range records {
			start, _ := time.Parse(time.RFC3339, rec.Start)
			runs = append(runs, jobRun{job: job.Name, rec: rec, start: start})
		}
		since, ok := runner.RunningSince(job.Name)
		if !ok || since.IsZero() {
			continue
		}
		// The lock outlives the final record by a hair: a just-finished Run can
		// still hold it after its record is on disk. Drop the synthetic row when
		// the newest record is that same Run, so it never shows up twice.
		if n := len(records); n > 0 {
			if last, err := time.Parse(time.RFC3339, records[n-1].Start); err == nil && last.Equal(since) {
				continue
			}
		}
		runs = append(runs, jobRun{
			job:     job.Name,
			rec:     runner.Record{Start: since.Format(time.RFC3339)},
			start:   since,
			running: true,
		})
	}
	if len(runs) == 0 {
		if name == "" {
			fmt.Println("No runs yet.")
		} else {
			fmt.Printf("No runs for %q yet.\n", name)
		}
		return nil
	}

	sort.SliceStable(runs, func(i, j int) bool {
		return runs[i].start.After(runs[j].start)
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}

	now := time.Now()
	t := historyTable()
	for _, run := range runs {
		when, passed := renderRunWhen(run.rec, run.start, now)
		status := renderStatus(run.rec.Status, run.rec.Reason, run.rec.Log)
		duration := renderRunDuration(run.rec)
		if run.running {
			status = runningStyle.Render(livePhaseRunning)
			duration = commentStyle.Render("—")
		}
		t.Row(cmdStyle.Render(run.job), when, passed, status, duration)
	}
	fmt.Print(renderTable(t))
	return nil
}

func printNoJobs() {
	fmt.Printf("No jobs in %s\n", config.DefaultPath())
}

// renderRunWhen produces the WHEN and PASSED cells from a Run's start: the
// displayed timestamp (the logs selector) and the elapsed-since "ago", which is
// blank when the start did not parse.
func renderRunWhen(rec runner.Record, start, now time.Time) (when, passed string) {
	when = commentStyle.Render(formatWhen(rec.Start))
	if start.IsZero() {
		return when, ""
	}
	return when, renderPassed(now.Sub(start))
}

func recDuration(rec runner.Record) time.Duration {
	return time.Duration(rec.DurationS) * time.Second
}

func renderRunDuration(rec runner.Record) string {
	if rec.Status == runner.StatusSkipped {
		return commentStyle.Render("—")
	}
	return commentStyle.Render(formatDuration(recDuration(rec)))
}

// resolveLog picks the Run whose output `logs` should show and reports whether
// it is in flight: the newest finished Run with output (no selector or
// "latest"), or the Run at a displayed timestamp. A Run is addressed by its
// fired time, never a positional index, so the timestamp the table prints
// round-trips back here as the selector. A timestamp also resolves the in-flight
// Run; "latest" stays on finished output, since --follow is the verb for
// attaching to an in-flight Run.
func resolveLog(job, selector string) (runner.Record, bool, error) {
	records, err := runner.History(job)
	if err != nil {
		return runner.Record{}, false, err
	}
	if selector != "" && selector != "latest" {
		return logByTimestamp(job, selector, records)
	}
	if len(records) == 0 {
		return runner.Record{}, false, fmt.Errorf("no runs for job %q", job)
	}
	rec, err := latestLog(job, records)
	return rec, false, err
}

func latestLog(job string, records []runner.Record) (runner.Record, error) {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Log != "" {
			return records[i], nil
		}
	}
	return runner.Record{}, fmt.Errorf("no captured output for job %q", job)
}

func logByTimestamp(job, timestamp string, records []runner.Record) (runner.Record, bool, error) {
	want, ok := parseSelectorTime(timestamp)
	if !ok {
		return runner.Record{}, false, fmt.Errorf("unrecognized timestamp %q for job %q (want %q)", timestamp, job, displayTimeFormat)
	}
	for _, rec := range records {
		start, err := time.Parse(time.RFC3339, rec.Start)
		if err != nil || !start.Equal(want) {
			continue
		}
		if rec.Log == "" {
			return runner.Record{}, false, fmt.Errorf("run at %q of job %q was skipped (%s); no output", timestamp, job, rec.Reason)
		}
		return rec, false, nil
	}
	if rec, ok := inflightRecord(job); ok {
		start, _ := time.Parse(time.RFC3339, rec.Start)
		if start.Equal(want) {
			return rec, true, nil
		}
	}
	return runner.Record{}, false, fmt.Errorf("no run %q for job %q", timestamp, job)
}

// inflightRecord builds a Record for the Job's in-flight agent phase from the
// live lock state, or reports false when no Run holds the lock or its start is
// not yet known (still in the Condition check, before the agent log exists).
func inflightRecord(job string) (runner.Record, bool) {
	since, ok := runner.RunningSince(job)
	if !ok || since.IsZero() {
		return runner.Record{}, false
	}
	logName, _ := runner.InFlight(job)
	if logName == "" {
		return runner.Record{}, false
	}
	return runner.Record{Start: since.Format(time.RFC3339), Log: logName}, true
}

// parseSelectorTime reads a timestamp selector in either the human-readable
// form acron displays or the legacy log-filename form. Both are local
// wall-clock, matching how runs are displayed and how log files are named.
func parseSelectorTime(timestamp string) (time.Time, bool) {
	timestamp = strings.TrimSuffix(timestamp, ".log")
	for _, layout := range []string{displayTimeFormat, runner.LogTimestampLayout} {
		if t, err := time.ParseInLocation(layout, timestamp, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func runConfigShow() error {
	path := config.DefaultPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; run \"acron config edit\" to create one", path)
	}
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func runEdit() error {
	path := config.DefaultPath()
	initial, err := initialBuffer(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpPath, err := writeTempConfig(filepath.Dir(path), initial)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		if err := openInEditor(tmpPath); err != nil {
			return err
		}
		if _, verr := loadAndValidate(tmpPath); verr != nil {
			fmt.Fprintln(os.Stderr, verr)
			retry, perr := promptRetry(scanner)
			if perr != nil {
				return perr
			}
			if !retry {
				return fmt.Errorf("edit aborted; %s unchanged", path)
			}
			continue
		}
		break
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	if bytes.Equal(edited, initial) {
		fmt.Println("No changes.")
		return nil
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	fmt.Printf("Saved %s\n", path)
	return nil
}

func initialBuffer(path string) ([]byte, error) {
	const configTemplate = `# acron config: each [[job]] schedules an agent to run on a cron schedule.
# Uncomment the example below, edit the values, and save. Field docs:
# https://github.com/wkentaro/acron
#
# [[job]]
# name     = "nightly-triage"             # required, unique, [a-z0-9_-]
# schedule = "0 2 * * *"                  # required, 5-field cron
# agent    = ["claude", "-p", "{prompt}", "--dangerously-skip-permissions", "--verbose", "--output-format", "stream-json"] # required argv; {prompt} is substituted
#   stream-json gives live output for long sessions; for a short session, drop --verbose/--output-format for plain-text logs
# prompt   = "Triage open issues"         # required
# cwd      = "~/src/acron"                # required, absolute or ~-expanded
# enabled  = true                         # optional, default true
# timeout  = "1h"                         # optional, default "1h"; 0 disables
# env      = { TZ = "Asia/Tokyo" }        # optional, extra environment vars
# condition = ["sh", "-c", "gh pr list | grep -q ."] # optional gate; skip agent unless exit 0
`
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []byte(configTemplate), nil
	}
	return data, err
}

func writeTempConfig(dir string, content []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, ".config.*.toml")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func openInEditor(path string) error {
	parts := append(strings.Fields(resolveEditor()), path)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func resolveEditor() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return "vi"
}

func promptRetry(scanner *bufio.Scanner) (bool, error) {
	fmt.Fprint(os.Stderr, "Return to edit? [Y/n] ")
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "" || answer == "y" || answer == "yes", nil
}

// renderPlan renders a Plan for printing. An empty plan is "Nothing to do.". A
// real apply or destroy prints the terse +/~/- summary. A dry-run apply renders
// each planned action as a git-style diff under its symbol line, so the whole
// plan is previewable in one read.
func renderPlan(plan *scheduler.Plan, header string, dryRun bool) string {
	if plan.Empty() {
		return "Nothing to do.\n"
	}
	var b strings.Builder
	fmt.Fprintln(&b, header)
	if dryRun {
		writePlanDiffs(&b, plan)
	} else {
		writePlanSummary(&b, plan)
	}
	return b.String()
}

func writePlanSummary(b *strings.Builder, plan *scheduler.Plan) {
	for _, name := range plan.Created {
		fmt.Fprintf(b, "  %s %s\n", addStyle.Render("+"), name)
	}
	for _, name := range plan.Updated {
		fmt.Fprintf(b, "  %s %s\n", runningStyle.Render("~"), name)
	}
	for _, name := range plan.Removed {
		fmt.Fprintf(b, "  %s %s\n", removeStyle.Render("-"), name)
	}
}

func writePlanDiffs(b *strings.Builder, plan *scheduler.Plan) {
	byName := make(map[string]scheduler.PlanChange, len(plan.Changes))
	for _, change := range plan.Changes {
		byName[change.Name] = change
	}
	var sections []string
	for _, name := range plan.Created {
		sections = append(sections, renderPlanChange(addStyle.Render("+"), name, byName[name]))
	}
	for _, name := range plan.Updated {
		sections = append(sections, renderPlanChange(runningStyle.Render("~"), name, byName[name]))
	}
	for _, name := range plan.Removed {
		sections = append(sections, renderPlanChange(removeStyle.Render("-"), name, byName[name]))
	}
	b.WriteString(strings.Join(sections, "\n"))
}

// renderPlanChange renders one planned action: a symbol-and-name line followed
// by a git-style diff for each changed unit, with a blank line between units.
// writePlanDiffs blank-line separates the sections too, so the plan reads job by
// job. A Job in the plan despite byte-identical units states that reason instead,
// since there is no textual diff to show.
func renderPlanChange(symbol, name string, change scheduler.PlanChange) string {
	if change.UnitsUnchanged {
		return fmt.Sprintf("  %s %s %s\n", symbol, name,
			commentStyle.Render("(units unchanged, would reload and restart)"))
	}
	header := fmt.Sprintf("  %s %s\n", symbol, name)
	var diffs []string
	for _, unit := range change.Units {
		if unit.Installed == unit.Desired {
			continue
		}
		diffs = append(diffs, renderUnitDiff(unit.Name, unit.Installed, unit.Desired))
	}
	return header + strings.Join(diffs, "\n")
}

// A skip that left a log is a condition skip whose check wrote to stderr, which
// almost always means the check broke (an unauthenticated gh, a typo'd command);
// it is lifted out of the dim skip style and annotated so it stands out from
// clean skips while scanning, pointing at `acron logs`. log is empty for clean
// skips, which stay dim.
func renderStatus(status runner.Status, reason runner.Reason, log string) string {
	var notes []string
	if reason != "" {
		notes = append(notes, string(reason))
	}
	style := statusStyle(status)
	if status == runner.StatusSkipped && log != "" {
		notes = append(notes, "output")
		style = warnStyle
	}
	label := string(status)
	if len(notes) > 0 {
		label += " (" + strings.Join(notes, ", ") + ")"
	}
	return style.Render(label)
}

func renderCommand(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = quoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	// Characters safe to leave unquoted, mirroring shlex.quote. This is a
	// display convenience; the agent itself runs from argv, not a shell.
	const safe = "-_./:@,=+%"
	for _, r := range arg {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') || strings.ContainsRune(safe, r) {
			continue
		}
		return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return arg
}

func statusStyle(status runner.Status) lipgloss.Style {
	switch status {
	case runner.StatusSuccess:
		return addStyle
	case runner.StatusSkipped, runner.StatusInterrupted:
		return commentStyle
	default:
		return removeStyle
	}
}

// Second precision so the displayed timestamp round-trips as a logs selector.
const displayTimeFormat = "2006-01-02 15:04:05"

func formatWhen(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format(displayTimeFormat)
}
