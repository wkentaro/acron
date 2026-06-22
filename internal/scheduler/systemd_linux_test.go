//go:build linux

package scheduler

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestApplyStates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	disabled := false
	job := func(name string, enabled *bool) config.Job {
		return config.Job{
			Name: name, Schedule: "0 2 * * *", Agent: []string{"true"},
			Prompt: "x", Cwd: "/tmp", Enabled: enabled,
		}
	}
	// A test-only name so the live-units case never collides with a real
	// acron-*.timer that systemctl might report active on a developer machine.
	live := job("applystate-test-live", nil)
	cfg := &config.Config{Jobs: []config.Job{
		job("pending", nil),
		job("off", &disabled),
		job("off-lingering", &disabled),
		live,
	}}

	// live gets current, matching units but no timer is ever loaded, so isActive
	// is false and its state is drifted rather than applied. off-lingering and
	// ghost get stale units; ghost is not in the Config.
	self, err := paths.Self()
	if err != nil {
		t.Fatal(err)
	}
	base, err := snapshotEnv()
	if err != nil {
		t.Fatal(err)
	}
	service, timer, err := renderJob(live, self, base)
	if err != nil {
		t.Fatal(err)
	}
	writeUnitsOrFail := func(name, svc, tmr string) {
		t.Helper()
		if err := os.WriteFile(paths.ServicePath(name), []byte(svc), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.TimerPath(name), []byte(tmr), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeUnitsOrFail("applystate-test-live", service, timer)
	writeUnitsOrFail("off-lingering", "svc", "tmr")
	writeUnitsOrFail("ghost", "svc", "tmr")

	states, err := ApplyStates(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []JobState{
		{Name: "pending", State: StateUnapplied},
		{Name: "off", State: StateDisabled},
		{Name: "off-lingering", State: StateDrifted},
		{Name: "applystate-test-live", State: StateDrifted}, // units match, but the timer is not loaded
		{Name: "ghost", State: StateOrphaned},
	}
	if !reflect.DeepEqual(states, want) {
		t.Errorf("ApplyStates = %+v\nwant %+v", states, want)
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
