//go:build darwin || linux

package scheduler

import (
	"maps"
	"os"
	"slices"
)

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
	maps.Copy(merged, base)
	maps.Copy(merged, extra)
	return merged
}

func sortedKeys(env map[string]string) []string {
	return slices.Sorted(maps.Keys(env))
}
