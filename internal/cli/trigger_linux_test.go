//go:build linux

package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/wkentaro/acron/internal/paths"
)

// setupTriggerStates writes a config and fabricates units so ApplyStates yields
// one job per non-applied state runTrigger rejects: unapplied (declared, no
// units), disabled (enabled = false), drifted (stale units), and orphaned (units
// with no config job). The applied path is not covered here: it ends in a real
// `systemctl start`, which a hermetic test cannot drive.
func setupTriggerStates(t *testing.T) {
	t.Helper()
	dir, path := setupEditTest(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(paths.SystemdUserDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	job := func(name, extra string) string {
		return "[[job]]\n" +
			"name = \"" + name + "\"\n" +
			"schedule = \"0 2 * * *\"\n" +
			"agent = [\"true\"]\n" +
			"prompt = \"x\"\n" +
			"cwd = \"" + dir + "\"\n" +
			extra
	}
	config := job("unapplied-job", "") +
		job("disabled-job", "enabled = false\n") +
		job("drifted-job", "")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	writeStaleUnits := func(name string) {
		if err := os.WriteFile(paths.ServicePath(name), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.TimerPath(name), []byte("stale"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeStaleUnits("drifted-job") // in config but units do not match: drifted
	writeStaleUnits("orphan-job")  // not in config: orphaned
}

func TestRunTriggerRejectsNonAppliedStates(t *testing.T) {
	setupTriggerStates(t)

	cases := []struct {
		name string
		want string
	}{
		{"missing-job", "no job named"},
		{"unapplied-job", "is not applied"},
		{"disabled-job", "is disabled"},
		{"drifted-job", "has drifted"},
		{"orphan-job", "is orphaned"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runTrigger(tc.name)
			if err == nil {
				t.Fatalf("runTrigger(%q) = nil, want error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("runTrigger(%q) = %q, want it to contain %q", tc.name, err, tc.want)
			}
		})
	}
}
