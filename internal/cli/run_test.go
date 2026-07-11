package cli

import (
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/runner"
)

func TestRunFooter(t *testing.T) {
	cases := []struct {
		name   string
		result runner.Result
		want   string
	}{
		{"success", runner.Result{Status: runner.StatusSuccess, Duration: 3 * time.Second}, "success  job  3s"},
		{"failure", runner.Result{Status: runner.StatusFailure, Exit: 3, Duration: 5 * time.Second}, "failure  job  exit 3  5s"},
		{"failure without exit code", runner.Result{Status: runner.StatusFailure, Exit: -1, Duration: time.Second}, "failure  job  1s"},
		{"timeout", runner.Result{Status: runner.StatusTimeout, Exit: -1, Duration: 30 * time.Minute}, "timeout  job  30min"},
		{"long run uses the house duration format", runner.Result{Status: runner.StatusSuccess, Duration: time.Hour + 35*time.Minute + 42*time.Second}, "success  job  1h 35min"},
		{"interrupted", runner.Result{Status: runner.StatusInterrupted, Exit: -1}, "interrupted  job  0s"},
		{"skipped", runner.Result{Status: runner.StatusSkipped, Reason: runner.ReasonCondition}, "skipped (condition)  job  0s"},
		{"suspect skip suppresses condition exit", runner.Result{Status: runner.StatusSkipped, Reason: runner.ReasonCondition, Exit: 127, LogPath: "x.log"}, "skipped (condition, output)  job  0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runFooter(tc.result, "job"); got != tc.want {
				t.Errorf("runFooter = %q, want %q", got, tc.want)
			}
		})
	}
}
