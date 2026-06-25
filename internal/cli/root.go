package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.0.0-dev" // set by -ldflags at build time

// errInterrupted marks a `run` aborted by Ctrl-C (SIGINT/SIGTERM). The Run is
// already recorded and reported as interrupted, so Execute only translates it
// into the conventional 130 exit code without printing a redundant error line.
var errInterrupted = errors.New("interrupted")

func Execute() {
	err := newRootCmd().Execute()
	if err == nil {
		return
	}
	if errors.Is(err, errInterrupted) {
		os.Exit(130)
	}
	fmt.Fprintln(os.Stderr, errorStyle.Render("error")+": "+err.Error())
	os.Exit(1)
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
