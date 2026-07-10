//go:build linux

package scheduler

import (
	"errors"
	"os"
	"os/exec"
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
		`ExecStart="/usr/local/bin/acron" run nightly-triage`,
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

func TestRenderServiceEscaping(t *testing.T) {
	tests := []struct {
		name string
		job  config.Job
		self string
		env  map[string]string
		want string
	}{
		{
			name: "percent in cwd is doubled",
			job:  config.Job{Name: "j", Cwd: "/home/user/50%_off"},
			self: "/usr/local/bin/acron",
			want: "WorkingDirectory=/home/user/50%%_off\n",
		},
		{
			name: "percent in env key is doubled",
			job:  config.Job{Name: "j", Cwd: "/tmp"},
			self: "/usr/local/bin/acron",
			env:  map[string]string{"PCT%KEY": "v"},
			want: `Environment="PCT%%KEY=v"` + "\n",
		},
		{
			name: "space in executable path stays one argument",
			job:  config.Job{Name: "j", Cwd: "/tmp"},
			self: "/opt/my apps/acron",
			want: `ExecStart="/opt/my apps/acron" run j` + "\n",
		},
		{
			name: "percent in executable path is doubled",
			job:  config.Job{Name: "j", Cwd: "/tmp"},
			self: "/opt/50%off/acron",
			want: `ExecStart="/opt/50%%off/acron" run j` + "\n",
		},
		{
			name: "backslash and quote in cwd are left literal",
			job:  config.Job{Name: "j", Cwd: `/home/a\b"c`},
			self: "/usr/local/bin/acron",
			want: `WorkingDirectory=/home/a\b"c` + "\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderService(tt.job, tt.self, tt.env)
			if !strings.Contains(out, tt.want) {
				t.Errorf("service missing %q\n---\n%s", tt.want, out)
			}
		})
	}
}

func TestRenderTimer(t *testing.T) {
	out := renderTimer("nightly-triage", []string{"*-*-* 02:00:00"})

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

func TestRenderTimerUnionsMultipleOnCalendar(t *testing.T) {
	out := renderTimer("weekly-or-monthly", []string{"*-*-15 09:00:00", "Mon *-*-* 09:00:00"})

	for _, want := range []string{
		"OnCalendar=*-*-15 09:00:00",
		"OnCalendar=Mon *-*-* 09:00:00",
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

func TestApplyDryRunPlan(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	job := func(name string) config.Job {
		return config.Job{
			Name: name, Schedule: "0 2 * * *", Agent: []string{"true"},
			Prompt: "x", Cwd: "/tmp",
		}
	}
	// "existing" has installed units, so apply would update it; "fresh" has none,
	// so apply would create it; "ghost" is owned but undeclared, so it is pruned.
	for _, name := range []string{"applydryrun-existing", "applydryrun-ghost"} {
		if err := os.WriteFile(paths.ServicePath(name), []byte("svc"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.TimerPath(name), []byte("tmr"), 0o644); err != nil {
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
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
	service, timer, err := renderJob(job, self, base)
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
	if units.Units[0].Desired != service || units.Units[0].Installed != "" {
		t.Error("uninstalled service: want desired set, installed empty")
	}
	if units.Units[1].Desired != timer || units.Units[1].Installed != "" {
		t.Error("uninstalled timer: want desired set, installed empty")
	}

	if err := os.WriteFile(paths.ServicePath("show-target"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TimerPath("show-target"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	units, err = Show(cfg, "show-target")
	if err != nil {
		t.Fatal(err)
	}
	if units.State != StateDrifted {
		t.Errorf("stale state = %q, want drifted", units.State)
	}
	if units.Units[0].Installed != "stale" || units.Units[0].Desired != service {
		t.Errorf("drifted service: got installed=%q desired set=%v", units.Units[0].Installed, units.Units[0].Desired == service)
	}

	if err := os.WriteFile(paths.ServicePath("ghost"), []byte("svc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TimerPath("ghost"), []byte("tmr"), 0o644); err != nil {
		t.Fatal(err)
	}
	units, err = Show(cfg, "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if units.State != StateOrphaned {
		t.Errorf("ghost state = %q, want orphaned", units.State)
	}
	if units.Units[0].Desired != "" || units.Units[0].Installed != "svc" {
		t.Errorf("orphaned service: got desired=%q installed=%q", units.Units[0].Desired, units.Units[0].Installed)
	}

	if _, err := Show(cfg, "nope"); err == nil {
		t.Error("Show on unknown job: want error, got nil")
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

func TestSystemctlWrapsExitError(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("systemctl not available")
	}
	err := systemctl("status", "acron-nonexistent-"+t.Name()+".timer")
	if err == nil {
		t.Fatal("expected an error for a nonexistent unit")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("error does not unwrap to *exec.ExitError: %v", err)
	}
}
