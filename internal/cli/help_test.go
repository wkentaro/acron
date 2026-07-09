package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

// pinPlainColor forces lipgloss to render without ANSI styling so help output
// can be asserted against exact strings regardless of the host's color profile.
// It mutates a process-global profile and restores it on cleanup; safe because
// the package's tests run serially.
func pinPlainColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestRenderHelpShowsCompletionInstallInstructions(t *testing.T) {
	root := newRootCmd()
	root.InitDefaultCompletionCmd()
	cmd, _, err := root.Find([]string{"completion", "zsh"})
	if err != nil {
		t.Fatalf("find completion zsh: %v", err)
	}

	out := renderHelp(cmd)

	for _, want := range []string{"compinit", "fpath", "source <("} {
		if !strings.Contains(out, want) {
			t.Errorf("zsh completion help missing %q in:\n%s", want, out)
		}
	}
}

func TestConfigHelpShowsJobSchema(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"config"})
	if err != nil {
		t.Fatalf("find config: %v", err)
	}

	out := renderHelp(cmd)

	for _, field := range []string{"[[job]]", "schedule", "agent", "prompt", "cwd", "enabled", "timeout", "env", "condition"} {
		if !strings.Contains(out, field) {
			t.Errorf("config help missing %q field:\n%s", field, out)
		}
	}
}

func TestRenderHelpFallsBackToShortWithoutLong(t *testing.T) {
	cmd := &cobra.Command{Short: "Show each job's apply state and last run"}

	out := renderHelp(cmd)

	if !strings.Contains(out, cmd.Short) {
		t.Errorf("renderHelp dropped Short when Long is empty:\n%s", out)
	}
}

func TestOptionRowsRendersFlagVariants(t *testing.T) {
	pinPlainColor(t)

	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().BoolP("follow", "f", false, "stream the run")
	cmd.Flags().Int("limit", 20, "cap the count")
	cmd.Flags().Bool("dry-run", false, "preview only")
	cmd.Flags().String("secret", "", "internal")
	if err := cmd.Flags().MarkHidden("secret"); err != nil {
		t.Fatalf("mark hidden: %v", err)
	}

	usage := make(map[string]string)
	for _, r := range optionRows(cmd) {
		if strings.Contains(r.left, "secret") {
			t.Errorf("hidden flag leaked into options: %q", r.left)
		}
		usage[r.left] = r.right
	}

	want := map[string]string{
		"-f, --follow":      "stream the run",
		"    --limit <int>": "cap the count",
		"    --dry-run":     "preview only",
	}
	for left, wantUsage := range want {
		gotUsage, ok := usage[left]
		if !ok {
			t.Errorf("option %q not rendered; got %v", left, usage)
			continue
		}
		if gotUsage != wantUsage {
			t.Errorf("option %q usage = %q, want %q", left, gotUsage, wantUsage)
		}
	}
}

func TestExampleRowsSplitsCommentsAndSkipsBlanks(t *testing.T) {
	pinPlainColor(t)

	rows := exampleRows("\nacron apply            # Reconcile units\nacron run nightly\n\n")

	if len(rows) != 2 {
		t.Fatalf("want 2 rows (blank lines skipped), got %d: %#v", len(rows), rows)
	}
	if rows[0].left != "acron apply" || rows[0].right != "# Reconcile units" {
		t.Errorf("commented example: left=%q right=%q", rows[0].left, rows[0].right)
	}
	if rows[1].left != "acron run nightly" || rows[1].right != "" {
		t.Errorf("uncommented example should have empty right: left=%q right=%q", rows[1].left, rows[1].right)
	}
}

func TestRenderHelpShowsOptionsAndExamples(t *testing.T) {
	pinPlainColor(t)

	out := renderHelp(newLogsCmd())

	for _, want := range []string{"Options:", "-f, --follow", "Examples:", "# Stream the run in progress until it finishes"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs help missing %q in:\n%s", want, out)
		}
	}
}
