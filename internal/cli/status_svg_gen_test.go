package cli

import (
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/runner"
	"github.com/wkentaro/acron/internal/scheduler"
)

// TestGenerateStatusANSI renders a representative `acron status` table to the
// file named by ACRON_STATUS_ANSI_OUT, for the README capture pipeline (the
// docs-status-svg make target pipes the output through freeze). It seeds the
// apply state and run history rather than reading them, so the capture needs no
// launchd units installed yet stays byte-faithful by going through the real
// render path.
func TestGenerateStatusANSI(t *testing.T) {
	out := os.Getenv("ACRON_STATUS_ANSI_OUT")
	if out == "" {
		t.Skip("set ACRON_STATUS_ANSI_OUT to regenerate the status capture")
	}
	// Force ANSI so the capture has color even though stdout is not a TTY; this
	// mutates a process-global, so restore it (the package's tests run serially).
	defaultProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(defaultProfile) })

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Fixed so the committed SVG is reproducible; do not replace with time.Now().
	now := time.Date(2026, 6, 30, 9, 15, 0, 0, time.UTC)

	seedRuns(t, "nightly-triage", []runner.Record{{
		Start:  now.Add(-7*time.Hour - 14*time.Minute - 57*time.Second).Format(time.RFC3339),
		Status: runner.StatusSuccess,
	}})
	seedRuns(t, "db-backup", []runner.Record{{
		Start:  now.Add(-45 * time.Minute).Format(time.RFC3339),
		Status: runner.StatusFailure,
	}})

	rows := []struct {
		job   config.Job
		state scheduler.ApplyState
	}{
		{config.Job{Name: "nightly-triage", Schedule: "0 2 * * *"}, scheduler.StateApplied},
		{config.Job{Name: "weekly-report", Schedule: "0 9 * * 1"}, scheduler.StateApplied},
		{config.Job{Name: "db-backup", Schedule: "*/30 * * * *"}, scheduler.StateDrifted},
	}

	tbl := statusTable()
	for _, r := range rows {
		status, last, passed, err := renderLastRun(r.job.Name, now)
		if err != nil {
			t.Fatal(err)
		}
		next, left := renderNext(r.job, r.state, now)
		tbl.Row(cmdStyle.Render(r.job.Name), renderApplyState(r.state), status, last, passed, next, left)
	}

	if err := os.WriteFile(out, []byte(renderTable(tbl)), 0o644); err != nil {
		t.Fatal(err)
	}
}
