package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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

func TestRenderHelpFallsBackToShortWithoutLong(t *testing.T) {
	cmd := &cobra.Command{Short: "Show each job's apply state and last run"}

	out := renderHelp(cmd)

	if !strings.Contains(out, cmd.Short) {
		t.Errorf("renderHelp dropped Short when Long is empty:\n%s", out)
	}
}
