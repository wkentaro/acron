//go:build darwin

package scheduler

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
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
		plist, err := renderJob(job, self, base)
		if err != nil {
			return nil, fmt.Errorf("apply %s: %w", job.Name, err)
		}
		if plistUnchanged(job.Name, plist) && isLoaded(job.Name) {
			continue
		}
		plan.Applied = append(plan.Applied, job.Name)
		if dryRun {
			continue
		}
		if err := applyJob(job.Name, plist); err != nil {
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
	return plan, nil
}

func renderJob(job config.Job, self string, base map[string]string) (string, error) {
	intervals, err := schedule.ToLaunchd(job.Schedule)
	if err != nil {
		return "", err
	}
	return renderPlist(job, self, intervals, mergeEnv(base, job.Env)), nil
}

func plistUnchanged(job, plist string) bool {
	existing, err := os.ReadFile(paths.PlistPath(job))
	return err == nil && string(existing) == plist
}

func isLoaded(job string) bool {
	return launchctl("print", domainTarget()+"/"+paths.PlistLabel(job)) == nil
}

func applyJob(job, plist string) error {
	if err := os.MkdirAll(paths.LaunchAgentsDir(), 0o755); err != nil {
		return err
	}
	path := paths.PlistPath(job)
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	bootout(job)
	return bootstrap(path)
}

func removeJob(name string) error {
	bootout(name)
	if err := os.Remove(paths.PlistPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ownedJobs() ([]string, error) {
	entries, err := os.ReadDir(paths.LaunchAgentsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []string
	for _, entry := range entries {
		if job, ok := paths.PlistJobName(entry.Name()); ok {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func domainTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func bootstrap(plistPath string) error {
	return launchctl("bootstrap", domainTarget(), plistPath)
}

// bootout ignores errors: the unit may simply not be loaded.
func bootout(job string) {
	_ = launchctl("bootout", domainTarget()+"/"+paths.PlistLabel(job))
}

func launchctl(args ...string) error {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func snapshotEnv() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	return map[string]string{
		"PATH": os.Getenv("PATH"),
		"HOME": home,
		"USER": user,
	}, nil
}

func mergeEnv(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func renderPlist(job config.Job, self string, intervals []schedule.CalendarInterval, env map[string]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")

	writeString(&b, "Label", paths.PlistLabel(job.Name))

	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range []string{self, "run", job.Name} {
		fmt.Fprintf(&b, "    <string>%s</string>\n", escape(arg))
	}
	b.WriteString("  </array>\n")

	writeString(&b, "WorkingDirectory", paths.ExpandHome(job.Cwd))

	b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "    <key>%s</key>\n    <string>%s</string>\n", escape(k), escape(env[k]))
	}
	b.WriteString("  </dict>\n")

	writeCalendar(&b, intervals)

	writeString(&b, "StandardOutPath", os.DevNull)
	writeString(&b, "StandardErrorPath", os.DevNull)

	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func writeString(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "  <key>%s</key>\n  <string>%s</string>\n", escape(key), escape(value))
}

func writeCalendar(b *strings.Builder, intervals []schedule.CalendarInterval) {
	b.WriteString("  <key>StartCalendarInterval</key>\n")
	if len(intervals) == 1 {
		writeCalendarDict(b, intervals[0], "  ")
		return
	}
	b.WriteString("  <array>\n")
	for _, ci := range intervals {
		writeCalendarDict(b, ci, "    ")
	}
	b.WriteString("  </array>\n")
}

func writeCalendarDict(b *strings.Builder, ci schedule.CalendarInterval, indent string) {
	fmt.Fprintf(b, "%s<dict>\n", indent)
	for _, kv := range []struct {
		key   string
		value *int
	}{
		{"Minute", ci.Minute},
		{"Hour", ci.Hour},
		{"Day", ci.Day},
		{"Weekday", ci.Weekday},
		{"Month", ci.Month},
	} {
		if kv.value != nil {
			fmt.Fprintf(b, "%s  <key>%s</key>\n%s  <integer>%d</integer>\n", indent, kv.key, indent, *kv.value)
		}
	}
	fmt.Fprintf(b, "%s</dict>\n", indent)
}

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escape(s string) string {
	return escaper.Replace(s)
}
