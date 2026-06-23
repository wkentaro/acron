package cli

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func complete(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{"__complete"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("__complete %v: %v", args, err)
	}
	return out.String()
}

func TestCompleteJobNamesListsNames(t *testing.T) {
	seedConfig(t, "nightly-triage", "weekly-report")
	noFileComp := fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp)
	for _, verb := range []string{"run", "trigger", "show", "logs", "history"} {
		out := complete(t, verb, "")
		for _, name := range []string{"nightly-triage", "weekly-report"} {
			if !strings.Contains(out, name) {
				t.Errorf("%s completion missing %q in:\n%s", verb, name, out)
			}
		}
		if strings.Contains(out, "\t") {
			t.Errorf("%s completion attached a description; schedules group confusingly in zsh:\n%s", verb, out)
		}
		if !strings.Contains(out, noFileComp) {
			t.Errorf("%s completion missing NoFileComp directive %q in:\n%s", verb, noFileComp, out)
		}
	}
}

func TestCompleteJobNamesStopsAfterFirstArg(t *testing.T) {
	seedConfig(t, "nightly-triage", "weekly-report")
	// logs is RangeArgs(1, 2), so cobra accepts a second arg; the guard in
	// completeJobNames is what must stop job-name completion there.
	for _, verb := range []string{"show", "logs"} {
		out := complete(t, verb, "nightly-triage", "")
		if strings.Contains(out, "weekly-report") {
			t.Errorf("%s offered a job name past the first arg:\n%s", verb, out)
		}
	}
}

func TestCompleteJobNamesSilentWithoutConfig(t *testing.T) {
	t.Setenv("ACRON_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	out := complete(t, "show", "")
	if strings.Contains(out, "nightly-triage") {
		t.Errorf("expected no candidates without a config, got:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "error") {
		t.Errorf("expected silent completion, got error text:\n%s", out)
	}
}
