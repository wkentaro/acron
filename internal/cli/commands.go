package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wkentaro/acron/internal/config"
)

func notImplemented(name string) error {
	return fmt.Errorf("%s is not implemented yet", name)
}

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Reconcile OS scheduler units to the config",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return notImplemented("apply")
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
			return notImplemented("destroy")
		},
	}
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <job>",
		Short: "Run a job now (the entry the scheduler invokes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error {
			return notImplemented("run")
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
			return notImplemented("status")
		},
	}
}

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <job>",
		Short: "Show a job's captured output",
		Args:  cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error {
			return notImplemented("logs")
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
			return notImplemented("edit")
		},
	}
}

func runList() error {
	path := config.DefaultPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if len(cfg.Jobs) == 0 {
		fmt.Printf("No jobs in %s\n", path)
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
