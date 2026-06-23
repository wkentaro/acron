package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
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

type completionJob struct {
	name   string
	prompt string
	cwd    string
}

func seedCompletionConfig(t *testing.T, jobs ...completionJob) {
	t.Helper()
	root := t.TempDir()
	var b strings.Builder
	for _, job := range jobs {
		cwd := job.cwd
		if cwd == "" {
			cwd = job.name
		}
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(root, cwd)
		}
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "[[job]]\nname = %q\nschedule = \"* * * * *\"\nagent = [\"echo\"]\nprompt = %q\ncwd = %q\n\n", job.name, job.prompt, cwd)
	}
	path := filepath.Join(root, "config.toml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACRON_CONFIG", path)
}

func TestCompleteJobNamesListsNamesAndHints(t *testing.T) {
	seedCompletionConfig(
		t,
		completionJob{name: "nightly-triage", prompt: "Triage open issues", cwd: "triage-repo"},
		completionJob{name: "weekly-report", prompt: "Prepare weekly report", cwd: "report-repo"},
	)
	noFileComp := fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp)
	for _, verb := range []string{"run", "trigger", "show", "logs", "history"} {
		out := complete(t, verb, "")
		for _, want := range []string{
			"nightly-triage\tTriage open issues — triage-repo",
			"weekly-report\tPrepare weekly report — report-repo",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s completion missing %q in:\n%s", verb, want, out)
			}
		}
		if !strings.Contains(out, noFileComp) {
			t.Errorf("%s completion missing NoFileComp directive %q in:\n%s", verb, noFileComp, out)
		}
	}
}

func TestCompleteJobNamesAlwaysAddsCwdBasename(t *testing.T) {
	seedCompletionConfig(
		t,
		completionJob{name: "acron-process-issues", prompt: "/process-issues", cwd: "repos/acron"},
		completionJob{name: "dotfiles-process-issues", prompt: "/process-issues", cwd: "repos/dotfiles"},
		completionJob{name: "acron-process-prs", prompt: "/process-prs", cwd: "repos/acron-prs"},
	)
	out := complete(t, "show", "")
	for _, want := range []string{
		"acron-process-issues\t/process-issues — acron",
		"dotfiles-process-issues\t/process-issues — dotfiles",
		"acron-process-prs\t/process-prs — acron-prs",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("completion missing %q in:\n%s", want, out)
		}
	}
}

func TestCompleteJobNamesKeepsCwdWhenPromptTruncated(t *testing.T) {
	seedCompletionConfig(
		t,
		completionJob{
			name:   "triage",
			prompt: "Run one issue-triage tick on the current repo's open issues, then stop.",
			cwd:    "myrepo",
		},
	)
	out := complete(t, "show", "")
	want := "triage\tRun one issue-triage tick on the current repo's open is… — myrepo"
	if !strings.Contains(out, want) {
		t.Fatalf("completion missing %q in:\n%s", want, out)
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

func TestPromptHint(t *testing.T) {
	got := promptHint("\n  First   line here  \nsecond line")
	if got != "First line here" {
		t.Fatalf("promptHint first line = %q, want %q", got, "First line here")
	}
}

func TestTruncateHint(t *testing.T) {
	got := truncateHint("Run one issue-triage tick on the current repo's open issues, then stop.")
	if got != "Run one issue-triage tick on the current repo's open is…" {
		t.Fatalf("truncateHint = %q", got)
	}
}
