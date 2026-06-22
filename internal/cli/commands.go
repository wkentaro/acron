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
		Use:   "run <job>",
		Short: "Run a job now (the entry the scheduler invokes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runJob(args[0])
		},
	}
}

func newTriggerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trigger <job>",
		Short: "Fire a job now, out of schedule, in the background",
		Args:  cobra.ExactArgs(1),
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

func newLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <job> [run]",
		Short: "Show a job's captured output",
		Args:  cobra.RangeArgs(1, 2),
		Example: `
acron logs nightly-triage                       # Newest run (same as "latest")
acron logs nightly-triage latest                # Newest run explicitly
acron logs nightly-triage 3                     # The 3rd most recent run (see acron history)
acron logs nightly-triage 2026-06-22T02-00-00   # A specific run by timestamp
`,
		RunE: func(_ *cobra.Command, args []string) error {
			selector := ""
			if len(args) == 2 {
				selector = args[1]
			}
			return runLogs(args[0], selector)
		},
	}
}

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <job>",
		Short: "List a job's past runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runHistory(args[0])
		},
	}
}

func newEditCmd() *cobra.Command {
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
	printPlan(plan, dryRun)
	return nil
}

func runDestroy() error {
	plan, err := scheduler.Destroy()
	if err != nil {
		return err
	}
	printPlan(plan, false)
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
		fmt.Printf("No jobs in %s\n", config.DefaultPath())
		return nil
	}
	t := statusTable()
	for _, st := range states {
		status, when, err := renderLastRun(st.Name)
		if err != nil {
			return err
		}
		t.Row(cmdStyle.Render(st.Name), renderApplyState(st.State), status, when)
	}
	fmt.Print(renderStatusTable(t))
	return nil
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
		commentStyle.Render("LAST RUN"),
		commentStyle.Render("WHEN"),
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

func renderLastRun(job string) (status, when string, err error) {
	if since, ok := runner.RunningSince(job); ok {
		status = runningStyle.Render("running")
		if !since.IsZero() {
			when = commentStyle.Render(since.Format("2006-01-02 15:04"))
		}
		return status, when, nil
	}
	rec, ok, err := runner.LastRecord(job)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return commentStyle.Render("never run"), "", nil
	}
	return renderStatus(rec.Status, rec.Reason), commentStyle.Render(formatWhen(rec.Start)), nil
}

func runLogs(job, selector string) error {
	name, err := resolveLog(job, selector)
	if err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(paths.RunsDir(job), name))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(os.Stdout, f)
	return err
}

func runHistory(job string) error {
	records, err := runner.History(job)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("no runs for job %q", job)
	}
	indexWidth := len(strconv.Itoa(len(records)))
	rows := make([]row, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		label := strings.TrimSuffix(rec.Log, ".log")
		if label == "" {
			label = formatWhen(rec.Start)
		}
		index := commentStyle.Render(fmt.Sprintf("%*d", indexWidth, len(records)-i))
		rows = append(rows, row{
			left:  index + "  " + argStyle.Render(label),
			right: renderStatus(rec.Status, rec.Reason),
		})
	}
	var b strings.Builder
	section(&b, "Runs:", rows)
	fmt.Print(b.String())
	return nil
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
	name := timestamp
	if !strings.HasSuffix(name, ".log") {
		name += ".log"
	}
	for _, rec := range records {
		if rec.Log == name {
			return rec.Log, nil
		}
	}
	return "", fmt.Errorf("no run %q for job %q", timestamp, job)
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

func printPlan(plan *scheduler.Plan, dryRun bool) {
	if len(plan.Applied) == 0 && len(plan.Removed) == 0 {
		fmt.Println("Nothing to do.")
		return
	}
	header := "Plan:"
	if dryRun {
		header = "Plan (dry run):"
	}
	fmt.Println(header)
	for _, name := range plan.Applied {
		fmt.Printf("  %s %s\n", addStyle.Render("+"), name)
	}
	for _, name := range plan.Removed {
		fmt.Printf("  %s %s\n", removeStyle.Render("-"), name)
	}
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

func formatWhen(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04")
}
