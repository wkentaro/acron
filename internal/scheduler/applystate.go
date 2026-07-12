//go:build darwin || linux

package scheduler

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
)

// pruneOrphans removes each acron-owned unit no longer desired by the Config,
// recording it in plan.Removed. On a dry run it captures the planned prune as a
// diff via removeChange instead of removing anything.
func pruneOrphans(plan *Plan, owned []string, desired map[string]bool, dryRun bool) error {
	for _, name := range owned {
		if desired[name] {
			continue
		}
		plan.Removed = append(plan.Removed, name)
		if dryRun {
			change, err := removeChange(name)
			if err != nil {
				return fmt.Errorf("remove %s: %w", name, err)
			}
			plan.Changes = append(plan.Changes, change)
			continue
		}
		if err := removeJob(name); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

// isOwned reports whether an acron-owned unit is installed for name, using the
// same scan apply and ApplyStates use.
func isOwned(name string) (bool, error) {
	owned, err := ownedJobs()
	if err != nil {
		return false, err
	}
	for _, n := range owned {
		if n == name {
			return true, nil
		}
	}
	return false, nil
}

// installedSet turns the acron-owned unit names into a membership set for
// O(1) "is this Job installed" lookups.
func installedSet(owned []string) map[string]bool {
	installed := make(map[string]bool, len(owned))
	for _, name := range owned {
		installed[name] = true
	}
	return installed
}

// readUnit returns a unit file's content, or "" when the file does not exist.
// Any other I/O error is returned to the caller.
func readUnit(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return string(content), nil
}

// ApplyStates reports each Job's ApplyState by performing the same comparison
// apply does, read-only, plus any orphaned acron-owned units no longer in the
// Config. Rows are the Config Jobs in order, followed by orphans sorted by name
// (ADR-0011).
func ApplyStates(cfg *config.Config) ([]JobState, error) {
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

	declared := make(map[string]bool, len(cfg.Jobs))
	states := make([]JobState, 0, len(cfg.Jobs)+len(owned))
	for _, job := range cfg.Jobs {
		declared[job.Name] = true
		state, err := jobApplyState(job, self, base, installed[job.Name])
		if err != nil {
			return nil, err
		}
		states = append(states, JobState{Name: job.Name, State: state})
	}

	var orphans []string
	for _, name := range owned {
		if !declared[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		states = append(states, JobState{Name: name, State: StateOrphaned})
	}
	return states, nil
}
