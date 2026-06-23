package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	names := make([]string, 0, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		names = append(names, job.Name+"\t"+job.Schedule)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
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
		Args:  cobra.NoArgs,
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
		Use:               "logs <job> [run]",
		Short:             "Show a job's captured output",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeJobNames,
		Example: `
acron logs nightly-triage                       # Newest run (same as "latest")
acron logs nightly-triage latest                # Newest run explicitly
acron logs nightly-triage 3                     # The 3rd most recent run (see acron history)
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
	return &cobra.Command{
		Use:               "history [job]",
		Short:             "List past runs",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeJobNames,
		Example: `
acron history                 # All jobs
acron history nightly-triage  # One job
`,
		RunE: func(_ *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runHistory(name)
		},
	}
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
	result, err := runner.Run(job)
	if err != nil {
		return err
	}
	fmt.Printf("%s  %s  exit %d  %s\n",
		renderStatus(result.Status, result.Reason), name, result.Exit, result.Duration.Round(time.Second))
	if result.LogPath != "" {
		fmt.Println(commentStyle.Render(result.LogPath))
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
	if runner.IsRunning(name) {
		fmt.Printf("%s  %s  %s\n", runningStyle.Render("running"), name,
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
	fmt.Print(renderStatusTable(t))
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

func renderStatusTable(t *table.Table) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(t.Render(), "\n"), "\n") {
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
	}
	return b.String()
}

func statusTable() *table.Table {
	headers := []string{
		commentStyle.Render("JOB"),
		commentStyle.Render("APPLY"),
		commentStyle.Render("STATUS"),
		commentStyle.Render("LAST"),
		commentStyle.Render("PASSED"),
		commentStyle.Render("NEXT"),
		commentStyle.Render("LEFT"),
	}
	return table.New().
		BorderTop(false).BorderBottom(false).BorderLeft(false).
		BorderRight(false).BorderColumn(false).BorderHeader(false).
		Headers(headers...).
		StyleFunc(func(_, col int) lipgloss.Style {
			if col < len(headers)-1 {
				return lipgloss.NewStyle().PaddingRight(2)
			}
			return lipgloss.NewStyle()
		})
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
// time to show: a never-run job, or an in-flight run still in its Condition
// check (unknown start). A running job's PASSED doubles as how long it has been
// running.
func renderLastRun(job string, now time.Time) (status, last, passed string, err error) {
	if since, ok := runner.RunningSince(job); ok {
		status = runningStyle.Render("running")
		if !since.IsZero() {
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
	status = renderStatus(rec.Status, rec.Reason)
	start, parseErr := time.Parse(time.RFC3339, rec.Start)
	if parseErr != nil {
		return status, commentStyle.Render(rec.Start), "", nil
	}
	return status, commentStyle.Render(start.Local().Format(displayTimeFormat)), renderPassed(now.Sub(start)), nil
}

func renderPassed(d time.Duration) string {
	return commentStyle.Render(formatDuration(d) + " ago")
}

func runLogs(job, selector string) error {
	if err := requireJob(job); err != nil {
		return err
	}
	name, err := resolveLog(job, selector)
	if err != nil {
		return err
	}
	return copyLog(job, name)
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
		return fmt.Errorf("--follow attaches to the running run; it cannot be combined with a run selector")
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
			fmt.Fprintln(os.Stderr, "waiting for agent to start...")
			notified = true
		}
		time.Sleep(followPollInterval)
	}
}

func streamLiveLog(job, logName string) error {
	f, err := os.Open(filepath.Join(paths.RunsDir(job), logName))
	if err != nil {
		return err
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
	if rec.Exit > 0 {
		notes = append(notes, fmt.Sprintf("exit %d", rec.Exit))
	}
	msg := "run " + string(rec.Status)
	if len(notes) > 0 {
		msg += " (" + strings.Join(notes, ", ") + ")"
	}
	if rec.Status == runner.StatusSkipped {
		return msg
	}
	return msg + " in " + (time.Duration(rec.DurationS) * time.Second).String()
}

func runHistory(name string) error {
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

	allRecords := make([][]runner.Record, len(jobs))
	indexWidth := 1
	for i, job := range jobs {
		records, err := runner.History(job.Name)
		if err != nil {
			return err
		}
		allRecords[i] = records
		if w := len(strconv.Itoa(len(records))); w > indexWidth {
			indexWidth = w
		}
	}

	sections := make([]string, len(jobs))
	for i, job := range jobs {
		sections[i] = renderJobHistory(job.Name, allRecords[i], indexWidth)
	}
	fmt.Print(strings.Join(sections, "\n"))
	return nil
}

func printNoJobs() {
	fmt.Printf("No jobs in %s\n", config.DefaultPath())
}

func renderJobHistory(jobName string, records []runner.Record, indexWidth int) string {
	var b strings.Builder
	if len(records) == 0 {
		section(&b, cmdStyle.Render(jobName), []row{{left: commentStyle.Render("never run")}})
		return b.String()
	}
	rows := make([]row, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		label := formatWhen(rec.Start)
		index := commentStyle.Render(fmt.Sprintf("%*d", indexWidth, len(records)-i))
		rows = append(rows, row{
			left:  index + "  " + argStyle.Render(label),
			right: renderStatus(rec.Status, rec.Reason),
		})
	}
	section(&b, cmdStyle.Render(jobName), rows)
	return b.String()
}

func resolveLog(job, selector string) (string, error) {
	records, err := runner.History(job)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", fmt.Errorf("no runs for job %q", job)
	}
	if selector == "" || selector == "latest" {
		return latestLog(job, records)
	}
	if index, err := strconv.Atoi(selector); err == nil {
		return logByIndex(job, records, index)
	}
	return logByTimestamp(job, selector, records)
}

func latestLog(job string, records []runner.Record) (string, error) {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Log != "" {
			return records[i].Log, nil
		}
	}
	return "", fmt.Errorf("no captured output for job %q", job)
}

func logByIndex(job string, records []runner.Record, index int) (string, error) {
	if index < 1 || index > len(records) {
		return "", fmt.Errorf("no run %d for job %q (have %d)", index, job, len(records))
	}
	rec := records[len(records)-index]
	if rec.Log == "" {
		return "", fmt.Errorf("run %d of job %q was skipped (%s); no output", index, job, rec.Reason)
	}
	return rec.Log, nil
}

func logByTimestamp(job, timestamp string, records []runner.Record) (string, error) {
	want, ok := parseSelectorTime(timestamp)
	if !ok {
		return "", fmt.Errorf("unrecognized timestamp %q for job %q (want %q)", timestamp, job, displayTimeFormat)
	}
	for _, rec := range records {
		start, err := time.Parse(time.RFC3339, rec.Start)
		if err != nil || !start.Equal(want) {
			continue
		}
		if rec.Log == "" {
			return "", fmt.Errorf("run at %q of job %q was skipped (%s); no output", timestamp, job, rec.Reason)
		}
		return rec.Log, nil
	}
	return "", fmt.Errorf("no run %q for job %q", timestamp, job)
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
# agent    = ["claude", "-p", "{prompt}"] # required argv; {prompt} is substituted
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

func renderStatus(status runner.Status, reason runner.Reason) string {
	label := string(status)
	if reason != "" {
		label += " (" + string(reason) + ")"
	}
	return statusStyle(status).Render(label)
}

func statusStyle(status runner.Status) lipgloss.Style {
	switch status {
	case runner.StatusSuccess:
		return addStyle
	case runner.StatusSkipped:
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
