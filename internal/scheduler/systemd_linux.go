//go:build linux

package scheduler

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
	"github.com/wkentaro/acron/internal/schedule"
)

func Apply(cfg *config.Config, dryRun bool) (*Plan, error) {
	self, err := paths.Self()
	if err != nil {
		return nil, err
	}
	base, err := snapshotEnv()
	if err != nil {
		return nil, err
	}

	plan := &Plan{}
	desired := make(map[string]bool)
	for _, job := range cfg.Jobs {
		if !job.IsEnabled() {
			continue
		}
		desired[job.Name] = true
		service, timer, err := renderJob(job, self, base)
		if err != nil {
			return nil, fmt.Errorf("apply %s: %w", job.Name, err)
		}
		if unitsUnchanged(job.Name, service, timer) && isActive(job.Name) {
			continue
		}
		plan.Applied = append(plan.Applied, job.Name)
		if dryRun {
			continue
		}
		if err := writeUnits(job.Name, service, timer); err != nil {
			return nil, fmt.Errorf("apply %s: %w", job.Name, err)
		}
	}

	owned, err := ownedJobs()
	if err != nil {
		return nil, err
	}
	for _, name := range owned {
		if desired[name] {
			continue
		}
		plan.Removed = append(plan.Removed, name)
		if dryRun {
			continue
		}
		if err := removeJob(name); err != nil {
			return nil, fmt.Errorf("remove %s: %w", name, err)
		}
	}

	if dryRun || (len(plan.Applied) == 0 && len(plan.Removed) == 0) {
		return plan, nil
	}
	if err := systemctl("daemon-reload"); err != nil {
		return nil, err
	}
	for _, name := range plan.Applied {
		if err := enableJob(name); err != nil {
			return nil, fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return plan, nil
}

func Destroy() (*Plan, error) {
	owned, err := ownedJobs()
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	for _, name := range owned {
		plan.Removed = append(plan.Removed, name)
		if err := removeJob(name); err != nil {
			return nil, fmt.Errorf("remove %s: %w", name, err)
		}
	}
	if len(plan.Removed) > 0 {
		if err := systemctl("daemon-reload"); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func renderJob(job config.Job, self string, base map[string]string) (service, timer string, err error) {
	onCalendar, err := schedule.ToSystemd(job.Schedule)
	if err != nil {
		return "", "", err
	}
	return renderService(job, self, mergeEnv(base, job.Env)), renderTimer(job.Name, onCalendar), nil
}

func unitsUnchanged(job, service, timer string) bool {
	return fileEquals(paths.ServicePath(job), service) && fileEquals(paths.TimerPath(job), timer)
}

func fileEquals(path, content string) bool {
	existing, err := os.ReadFile(path)
	return err == nil && string(existing) == content
}

func isActive(job string) bool {
	return systemctl("is-active", "--quiet", paths.TimerName(job)) == nil
}

func writeUnits(job, service, timer string) error {
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(paths.ServicePath(job), []byte(service), 0o644); err != nil {
		return err
	}
	return os.WriteFile(paths.TimerPath(job), []byte(timer), 0o644)
}

func enableJob(job string) error {
	if err := systemctl("enable", paths.TimerName(job)); err != nil {
		return err
	}
	return systemctl("restart", paths.TimerName(job))
}

func removeJob(name string) error {
	disable(name)
	for _, path := range []string{paths.TimerPath(name), paths.ServicePath(name)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func ownedJobs() ([]string, error) {
	entries, err := os.ReadDir(paths.SystemdUserDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "acron-") && strings.HasSuffix(name, ".timer") {
			jobs = append(jobs, strings.TrimSuffix(strings.TrimPrefix(name, "acron-"), ".timer"))
		}
	}
	return jobs, nil
}

// disable ignores errors: the timer may simply not be loaded.
func disable(job string) {
	_ = systemctl("disable", "--now", paths.TimerName(job))
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func renderService(job config.Job, self string, env map[string]string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=acron job %s\n\n", job.Name)
	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	fmt.Fprintf(&b, "ExecStart=%s run %s\n", self, job.Name)
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", paths.ExpandHome(job.Cwd))
	b.WriteString("StandardOutput=null\n")
	b.WriteString("StandardError=null\n")

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", k, escapeEnv(env[k]))
	}
	return b.String()
}

func renderTimer(job, onCalendar string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=acron job %s\n\n", job)
	b.WriteString("[Timer]\n")
	fmt.Fprintf(&b, "OnCalendar=%s\n", onCalendar)
	b.WriteString("Persistent=true\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")
	return b.String()
}

var envEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "%", "%%")

func escapeEnv(s string) string {
	return envEscaper.Replace(s)
}
