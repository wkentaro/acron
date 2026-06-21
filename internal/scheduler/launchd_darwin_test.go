//go:build darwin

package scheduler

import (
	"os"
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

func TestEscape(t *testing.T) {
	if got := escape("a & b < c > d"); got != "a &amp; b &lt; c &gt; d" {
		t.Errorf("escape = %q", got)
	}
}
