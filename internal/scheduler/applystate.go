//go:build darwin || linux

package scheduler

import (
	"sort"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/paths"
)

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
	installed := make(map[string]bool, len(owned))
	for _, name := range owned {
		installed[name] = true
	}

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
