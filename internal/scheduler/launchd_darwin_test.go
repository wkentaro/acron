//go:build darwin

package scheduler

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/schedule"
)

func TestRenderPlist(t *testing.T) {
	job := config.Job{
		Name:   "nightly-triage",
		Agent:  []string{"claude", "-p", "{prompt}"},
		Prompt: "Triage open issues",
		Cwd:    "/tmp/repo",
	}
	intervals, err := schedule.ToLaunchd("0 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": "/usr/bin", "HOME": "/Users/x"}

	out := renderPlist(job, "/usr/local/bin/acron", intervals, env)

	for _, want := range []string{
		"<key>Label</key>\n  <string>com.acron.nightly-triage</string>",
		"<string>/usr/local/bin/acron</string>",
		"<string>run</string>",
		"<string>nightly-triage</string>",
		"<key>WorkingDirectory</key>\n  <string>/tmp/repo</string>",
		"<key>Minute</key>\n    <integer>0</integer>",
		"<key>Hour</key>\n    <integer>2</integer>",
		"<key>PATH</key>\n    <string>/usr/bin</string>",
		"<key>StandardOutPath</key>\n  <string>/dev/null</string>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "<key>Day</key>") {
		t.Errorf("plist should omit unset Day field\n---\n%s", out)
	}
}

func TestPlistUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PlistPath("nightly"), []byte("<plist>x</plist>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !plistUnchanged("nightly", "<plist>x</plist>") {
		t.Error("expected unchanged for identical content")
	}
	if plistUnchanged("nightly", "<plist>y</plist>") {
		t.Error("expected changed for differing content")
	}
	if plistUnchanged("missing", "<plist>x</plist>") {
		t.Error("expected changed when plist is absent")
	}
}

func TestApplyDryRunPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	job := func(name string) config.Job {
		return config.Job{
			Name: name, Schedule: "0 2 * * *", Agent: []string{"true"},
			Prompt: "x", Cwd: "/tmp",
		}
	}
	// "existing" has an installed plist, so apply would update it; "fresh" has
	// none, so apply would create it; "ghost" is owned but undeclared, so it is
	// pruned.
	for _, name := range []string{"applydryrun-existing", "applydryrun-ghost"} {
		if err := os.WriteFile(paths.PlistPath(name), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Jobs: []config.Job{job("applydryrun-fresh"), job("applydryrun-existing")}}

	plan, err := Apply(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Created, []string{"applydryrun-fresh"}) {
		t.Errorf("Created = %v, want [applydryrun-fresh]", plan.Created)
	}
	if !reflect.DeepEqual(plan.Updated, []string{"applydryrun-existing"}) {
		t.Errorf("Updated = %v, want [applydryrun-existing]", plan.Updated)
	}
	if !reflect.DeepEqual(plan.Removed, []string{"applydryrun-ghost"}) {
		t.Errorf("Removed = %v, want [applydryrun-ghost]", plan.Removed)
	}
}

func TestShow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	job := config.Job{
		Name: "show-target", Schedule: "0 2 * * *", Agent: []string{"true"},
		Prompt: "x", Cwd: "/tmp",
	}
	cfg := &config.Config{Jobs: []config.Job{job}}
	self, err := paths.Self()
	if err != nil {
		t.Fatal(err)
	}
	base, err := snapshotEnv()
	if err != nil {
		t.Fatal(err)
	}
	plist, err := renderJob(job, self, base)
	if err != nil {
		t.Fatal(err)
	}

	units, err := Show(cfg, "show-target")
	if err != nil {
		t.Fatal(err)
	}
	if units.State != StateUnapplied {
		t.Errorf("uninstalled state = %q, want unapplied", units.State)
	}
	if units.Units[0].Desired != plist || units.Units[0].Installed != "" {
		t.Error("uninstalled plist: want desired set, installed empty")
	}

	if err := os.WriteFile(paths.PlistPath("show-target"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	units, err = Show(cfg, "show-target")
	if err != nil {
		t.Fatal(err)
	}
	if units.State != StateDrifted {
		t.Errorf("stale state = %q, want drifted", units.State)
	}
	if units.Units[0].Installed != "stale" || units.Units[0].Desired != plist {
		t.Errorf("drifted plist: got installed=%q desired set=%v", units.Units[0].Installed, units.Units[0].Desired == plist)
	}

	if err := os.WriteFile(paths.PlistPath("ghost"), []byte("<plist>x</plist>"), 0o644); err != nil {
		t.Fatal(err)
	}
	units, err = Show(cfg, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if units.State != StateOrphaned {
		t.Errorf("ghost state = %q, want orphaned", units.State)
	}
	if units.Units[0].Desired != "" || units.Units[0].Installed != "<plist>x</plist>" {
		t.Errorf("orphaned plist: got desired=%q installed=%q", units.Units[0].Desired, units.Units[0].Installed)
	}

	if _, err := Show(cfg, "nope"); err == nil {
		t.Error("Show on unknown job: want error, got nil")
	}
}

func TestEscape(t *testing.T) {
	if got := escape("a & b < c > d"); got != "a &amp; b &lt; c &gt; d" {
		t.Errorf("escape = %q", got)
	}
}

func TestLaunchctlWrapsExitError(t *testing.T) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		t.Skip("launchctl not available")
	}
	err := launchctl("print", "system/acron-nonexistent-"+t.Name())
	if err == nil {
		t.Fatal("expected an error for a nonexistent service")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error does not unwrap to *exec.ExitError: %v", err)
	}
}
