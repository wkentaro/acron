package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const version = "0.0.0-dev"

func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, errorStyle.Render("error")+": "+err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cobra.EnableCommandSorting = false
	root := &cobra.Command{
		Use:           "acron",
		Short:         "Schedule an agent prompt to run periodically across systemd and launchd.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `
acron apply                 # Reconcile units to the config
acron run nightly-triage    # Run a job now in the foreground
acron trigger nightly-triage # Fire a job now in the background
acron status                # Show apply state and last run per job
acron show nightly-triage   # Show a job's generated unit and any drift
acron logs nightly-triage   # Show a job's latest run
acron history nightly-triage # List a job's past runs
acron destroy               # Remove all acron units (keep config)
`,
	}
	root.PersistentFlags().BoolP("help", "h", false, "Print help")
	root.Flags().BoolP("version", "V", false, "Print version")
	root.SetVersionTemplate("acron " + commentStyle.Render(version) + "\n")

	root.AddCommand(
		newApplyCmd(),
		newDestroyCmd(),
		newRunCmd(),
		newTriggerCmd(),
		newStatusCmd(),
		newShowCmd(),
		newLogsCmd(),
		newHistoryCmd(),
		newConfigCmd(),
	)

	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		fmt.Print(renderHelp(cmd))
	})
	return root
}
