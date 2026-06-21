package scheduler

// Plan reports what an Apply or Destroy did (or would do, under dry run).
type Plan struct {
	Applied []string
	Removed []string
}
