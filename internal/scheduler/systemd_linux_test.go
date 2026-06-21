//go:build linux

package scheduler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
)

func TestRenderService(t *testing.T) {
	job := config.Job{
		Name:   "nightly-triage",
		Agent:  []string{"claude", "-p", "{prompt}"},
		Prompt: "Triage open issues",
		Cwd:    "/tmp/repo",
	}
	env := map[string]string{"PATH": "/usr/bin", "HOME": "/home/x"}

	out := renderService(job, "/usr/local/bin/acron", env)

	for _, want := range []string{
		"[Service]",
		"Type=oneshot",
		"ExecStart=/usr/local/bin/acron run nightly-triage",
		"WorkingDirectory=/tmp/repo",
		"StandardOutput=null",
		"StandardError=null",
		`Environment="PATH=/usr/bin"`,
		`Environment="HOME=/home/x"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("service missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderTimer(t *testing.T) {
	out := renderTimer("nightly-triage", "*-*-* 02:00:00")

	for _, want := range []string{
		"[Timer]",
		"OnCalendar=*-*-* 02:00:00",
		"Persistent=true",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("timer missing %q\n---\n%s", want, out)
		}
	}
}

func TestUnitsUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ServicePath("nightly"), []byte("svc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TimerPath("nightly"), []byte("tmr"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !unitsUnchanged("nightly", "svc", "tmr") {
		t.Error("expected unchanged for identical content")
	}
	if unitsUnchanged("nightly", "svc", "different") {
		t.Error("expected changed for differing timer")
	}
	if unitsUnchanged("missing", "svc", "tmr") {
		t.Error("expected changed when units are absent")
	}
}

func TestOwnedJobs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		paths.TimerName("alpha"),
		paths.ServiceName("alpha"),
		paths.TimerName("beta"),
		"unrelated.timer",
	} {
		if err := os.WriteFile(filepath.Join(paths.SystemdUserDir(), name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	jobs, err := ownedJobs()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(jobs, ",")
	if got != "alpha,beta" {
		t.Errorf("ownedJobs = %q, want \"alpha,beta\"", got)
	}
}

func TestEscapeEnv(t *testing.T) {
	if got := escapeEnv(`a"b\c`); got != `a\"b\\c` {
		t.Errorf("escapeEnv = %q", got)
	}
	if got := escapeEnv("a%hb"); got != "a%%hb" {
		t.Errorf("escapeEnv = %q", got)
	}
}
