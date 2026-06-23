package scheduler

// Plan reports what an Apply or Destroy did (or would do, under dry run). A
// converging Job is Created when no unit is yet installed for it and Updated
// when an installed unit would be rewritten or restarted.
type Plan struct {
	Created []string
	Updated []string
	Removed []string
}

// Empty reports whether the Plan would change nothing.
func (p *Plan) Empty() bool {
	return len(p.Created)+len(p.Updated)+len(p.Removed) == 0
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

// UnitFile is one OS scheduler file acron manages for a Job: its identifying
// name, the content rendered from the Config (Desired), and the content
// installed on this machine (Installed). Either may be empty: an orphaned Job
// has no Desired, an unapplied Job has no Installed.
type UnitFile struct {
	Name      string
	Desired   string
	Installed string
}

// JobUnits is what show reports for one Job: its ApplyState and the unit files
// acron manages for it.
type JobUnits struct {
	Name  string
	State ApplyState
	Units []UnitFile
}
