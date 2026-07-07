package paths

import (
	"os"
	"path/filepath"
	"strings"
)

func StateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "acron")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "acron")
}

func RunsDir(job string) string {
	return filepath.Join(StateDir(), "runs", job)
}

func HistoryPath(job string) string {
	return filepath.Join(RunsDir(job), "history.jsonl")
}

func LocksDir() string {
	return filepath.Join(StateDir(), "locks")
}

func LockPath(job string) string {
	return filepath.Join(LocksDir(), job+".lock")
}

func LaunchAgentsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents")
}

const labelPrefix = "com.acron."

func PlistLabel(job string) string {
	return labelPrefix + job
}

func PlistJobName(filename string) (string, bool) {
	base, ok := strings.CutSuffix(filename, ".plist")
	if !ok {
		return "", false
	}
	return strings.CutPrefix(base, labelPrefix)
}

func PlistPath(job string) string {
	return filepath.Join(LaunchAgentsDir(), PlistLabel(job)+".plist")
}

func SystemdUserDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user")
}

const unitPrefix = "acron-"

func ServiceName(job string) string {
	return unitPrefix + job + ".service"
}

func TimerName(job string) string {
	return unitPrefix + job + ".timer"
}

func TimerJobName(filename string) (string, bool) {
	base, ok := strings.CutSuffix(filename, ".timer")
	if !ok {
		return "", false
	}
	return strings.CutPrefix(base, unitPrefix)
}

func ServicePath(job string) string {
	return filepath.Join(SystemdUserDir(), ServiceName(job))
}

func TimerPath(job string) string {
	return filepath.Join(SystemdUserDir(), TimerName(job))
}

func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

func ExpandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}
