package scheduler

// Plan reports what an Apply or Destroy did (or would do, under dry run).
type Plan struct {
	Applied []string
	Removed []string
}

// ApplyState is a Job's standing relative to apply: applied exactly when apply
// would be a no-op for it (ADR-0011).
type ApplyState string

const (
	StateApplied   ApplyState = "applied"
	StateDrifted   ApplyState = "drifted"
	StateUnapplied ApplyState = "unapplied"
	StateOrphaned  ApplyState = "orphaned"
	StateDisabled  ApplyState = "disabled"
)

// JobState pairs a Job (or orphaned unit) name with its ApplyState.
type JobState struct {
	Name  string
	State ApplyState
}
