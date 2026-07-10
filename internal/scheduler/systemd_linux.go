//go:build linux

package scheduler

import (
	"fmt"
	"os"
	"os/exec"
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
	owned, err := ownedJobs()
	if err != nil {
		return nil, err
	}
	installed := installedSet(owned)

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
		unchanged := unitsUnchanged(job.Name, service, timer)
		if unchanged && isActive(job.Name) {
			continue
		}
		if installed[job.Name] {
			plan.Updated = append(plan.Updated, job.Name)
		} else {
			plan.Created = append(plan.Created, job.Name)
		}
		if dryRun {
			change, err := convergeChange(job.Name, service, timer, installed[job.Name], unchanged)
			if err != nil {
				return nil, fmt.Errorf("apply %s: %w", job.Name, err)
			}
			plan.Changes = append(plan.Changes, change)
			continue
		}
		if err := writeUnits(job.Name, service, timer); err != nil {
			return nil, fmt.Errorf("apply %s: %w", job.Name, err)
		}
	}

	for _, name := range owned {
		if desired[name] {
			continue
		}
		plan.Removed = append(plan.Removed, name)
		if dryRun {
			change, err := removeChange(name)
			if err != nil {
				return nil, fmt.Errorf("remove %s: %w", name, err)
			}
			plan.Changes = append(plan.Changes, change)
			continue
		}
		if err := removeJob(name); err != nil {
			return nil, fmt.Errorf("remove %s: %w", name, err)
		}
	}

	if dryRun || plan.Empty() {
		return plan, nil
	}
	if err := systemctl("daemon-reload"); err != nil {
		return nil, err
	}
	converged := append(append([]string{}, plan.Created...), plan.Updated...)
	for _, name := range converged {
		if err := enableJob(name); err != nil {
			return nil, fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return plan, nil
}

// Show reports a Job's generated units (rendered from the Config) alongside the
// content installed on this machine and the Job's ApplyState, so the caller can
// diff what apply would write against what is already there.
func Show(cfg *config.Config, name string) (*JobUnits, error) {
	self, err := paths.Self()
	if err != nil {
		return nil, err
	}
	base, err := snapshotEnv()
	if err != nil {
		return nil, err
	}
	installed, err := isOwned(name)
	if err != nil {
		return nil, err
	}

	job, err := cfg.Job(name)
	if err != nil {
		if !installed {
			return nil, err
		}
		svcContent, tmrContent, err := readInstalledUnits(name)
		if err != nil {
			return nil, err
		}
		return &JobUnits{Name: name, State: StateOrphaned, Units: []UnitFile{
			{Name: paths.ServiceName(name), Installed: svcContent},
			{Name: paths.TimerName(name), Installed: tmrContent},
		}}, nil
	}

	state, err := jobApplyState(job, self, base, installed)
	if err != nil {
		return nil, err
	}
	service, timer, err := renderJob(job, self, base)
	if err != nil {
		return nil, err
	}
	svcContent, tmrContent, err := readInstalledUnits(name)
	if err != nil {
		return nil, err
	}
	return &JobUnits{Name: name, State: state, Units: []UnitFile{
		{Name: paths.ServiceName(name), Desired: service, Installed: svcContent},
		{Name: paths.TimerName(name), Desired: timer, Installed: tmrContent},
	}}, nil
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

func jobApplyState(job config.Job, self string, base map[string]string, installed bool) (ApplyState, error) {
	return applyStateFrom(job.IsEnabled(), installed, func() (bool, error) {
		service, timer, err := renderJob(job, self, base)
		if err != nil {
			return false, err
		}
		return unitsUnchanged(job.Name, service, timer) && isActive(job.Name), nil
	})
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

// convergeChange captures a created or updated Job's unit content so a dry-run
// caller can render the planned write as a diff. installed is false for a create
// (nothing on disk to read); unchanged marks the inactive-timer case, where the
// units are byte-identical and apply would only reload and restart, so the
// caller states that reason rather than rendering a diff and the content is left
// unread.
func convergeChange(job, service, timer string, installed, unchanged bool) (PlanChange, error) {
	svcInstalled, tmrInstalled := "", ""
	if installed && !unchanged {
		var err error
		if svcInstalled, tmrInstalled, err = readInstalledUnits(job); err != nil {
			return PlanChange{}, err
		}
	}
	return PlanChange{
		Name:           job,
		UnitsUnchanged: unchanged,
		Units: []UnitFile{
			{Name: paths.ServiceName(job), Desired: service, Installed: svcInstalled},
			{Name: paths.TimerName(job), Desired: timer, Installed: tmrInstalled},
		},
	}, nil
}

// removeChange captures an orphaned Job's installed unit content so a dry-run
// caller can render the planned prune as an all-red diff against /dev/null.
func removeChange(job string) (PlanChange, error) {
	svc, tmr, err := readInstalledUnits(job)
	if err != nil {
		return PlanChange{}, err
	}
	return PlanChange{
		Name: job,
		Units: []UnitFile{
			{Name: paths.ServiceName(job), Installed: svc},
			{Name: paths.TimerName(job), Installed: tmr},
		},
	}, nil
}

// readInstalledUnits reads the installed service and timer content for a Job,
// propagating any non-NotExist read error.
func readInstalledUnits(job string) (service, timer string, err error) {
	if service, err = readUnit(paths.ServicePath(job)); err != nil {
		return "", "", err
	}
	if timer, err = readUnit(paths.TimerPath(job)); err != nil {
		return "", "", err
	}
	return service, timer, nil
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
		if job, ok := paths.TimerJobName(entry.Name()); ok {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// disable ignores errors: the timer may simply not be loaded.
func disable(job string) {
	_ = systemctl("disable", "--now", paths.TimerName(job))
}

// Trigger starts the Job's service unit now, out of schedule. --no-block returns
// without waiting for the oneshot to finish, so the Run is detached.
func Trigger(job string) error {
	return systemctl("start", "--no-block", paths.ServiceName(job))
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
	fmt.Fprintf(&b, "ExecStart=\"%s\" run %s\n", escapeEnv(self), job.Name)
	// WorkingDirectory is specifier-expanded but not C-unescaped, so only "%" is
	// a metacharacter; backslashes and quotes are literal (envEscaper would corrupt them).
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", strings.ReplaceAll(paths.ExpandHome(job.Cwd), "%", "%%"))
	b.WriteString("StandardOutput=null\n")
	b.WriteString("StandardError=null\n")

	for _, k := range sortedKeys(env) {
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", escapeEnv(k), escapeEnv(env[k]))
	}
	return b.String()
}

func renderTimer(job string, onCalendar []string) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=acron job %s\n\n", job)
	b.WriteString("[Timer]\n")
	for _, line := range onCalendar {
		fmt.Fprintf(&b, "OnCalendar=%s\n", line)
	}
	b.WriteString("Persistent=true\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")
	return b.String()
}

// envEscaper escapes a string for a double-quoted, C-unescaped, specifier-expanded
// context (ExecStart args and Environment assignments).
var envEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "%", "%%")

func escapeEnv(s string) string {
	return envEscaper.Replace(s)
}
