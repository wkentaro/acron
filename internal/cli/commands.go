package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/runner"
	"github.com/wkentaro/acron/internal/scheduler"
)

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(config.DefaultPath())
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

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List jobs from the config",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runList()
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show each job's last run status",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runStatus()
		},
	}
}

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <job>",
		Short: "Show a job's captured output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, _ := cmd.Flags().GetString("run")
			list, _ := cmd.Flags().GetBool("list")
			return runLogs(args[0], run, list)
		},
	}
	cmd.Flags().String("run", "", "Show a specific run by timestamp")
	cmd.Flags().Bool("list", false, "List runs instead of showing output")
	return cmd
}

func newEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the config in $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("edit is not implemented yet")
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
	for _, job := range cfg.Jobs {
		if job.Name != name {
			continue
		}
		result, err := runner.Run(job)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s  exit %d  %s\n",
			statusStyle(result.Status).Render(string(result.Status)), name, result.Exit, result.Duration.Round(time.Second))
		if result.LogPath != "" {
			fmt.Println(commentStyle.Render(result.LogPath))
		}
		return nil
	}
	return fmt.Errorf("no job named %q", name)
}

func runList() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Jobs) == 0 {
		fmt.Printf("No jobs in %s\n", config.DefaultPath())
		return nil
	}
	rows := make([]row, 0, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		schedule := job.Schedule
		if !job.IsEnabled() {
			schedule += commentStyle.Render("  (disabled)")
		}
		rows = append(rows, row{left: cmdStyle.Render(job.Name), right: schedule})
	}
	var b strings.Builder
	section(&b, "Jobs:", rows)
	fmt.Print(b.String())
	return nil
}

func runStatus() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Jobs) == 0 {
		fmt.Printf("No jobs in %s\n", config.DefaultPath())
		return nil
	}
	rows := make([]row, 0, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		rec, ok, err := runner.LastRecord(job.Name)
		if err != nil {
			return err
		}
		right := commentStyle.Render("never run")
		if ok {
			right = statusStyle(rec.Status).Render(string(rec.Status)) + "  " + commentStyle.Render(formatWhen(rec.Start))
		}
		rows = append(rows, row{left: cmdStyle.Render(job.Name), right: right})
	}
	var b strings.Builder
	section(&b, "Status:", rows)
	fmt.Print(b.String())
	return nil
}

func runLogs(job, run string, list bool) error {
	if list {
		return listRuns(job)
	}
	name, err := resolveLog(job, run)
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

func listRuns(job string) error {
	records, err := runner.History(job)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("no runs for job %q", job)
	}
	rows := make([]row, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		label := strings.TrimSuffix(rec.Log, ".log")
		if label == "" {
			label = formatWhen(rec.Start)
		}
		rows = append(rows, row{
			left:  argStyle.Render(label),
			right: statusStyle(rec.Status).Render(string(rec.Status)),
		})
	}
	var b strings.Builder
	section(&b, "Runs:", rows)
	fmt.Print(b.String())
	return nil
}

func resolveLog(job, run string) (string, error) {
	if run != "" {
		name := run
		if !strings.HasSuffix(name, ".log") {
			name += ".log"
		}
		if _, err := os.Stat(filepath.Join(paths.RunsDir(job), name)); err != nil {
			return "", fmt.Errorf("no run %q for job %q", run, job)
		}
		return name, nil
	}
	return newestLog(job)
}

func newestLog(job string) (string, error) {
	entries, err := os.ReadDir(paths.RunsDir(job))
	if err != nil {
		return "", fmt.Errorf("no runs for job %q", job)
	}
	newest := ""
	for _, entry := range entries {
		if name := entry.Name(); strings.HasSuffix(name, ".log") && name > newest {
			newest = name
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no runs for job %q", job)
	}
	return newest, nil
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
