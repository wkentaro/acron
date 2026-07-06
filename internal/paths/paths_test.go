package paths

import (
	"path/filepath"
	"testing"
)

func TestStateDir(t *testing.T) {
	t.Run("XDG_STATE_HOME wins", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "/xdg/state")
		want := filepath.Join("/xdg/state", "acron")
		if got := StateDir(); got != want {
			t.Errorf("StateDir() = %q, want %q", got, want)
		}
	})
	t.Run("falls back to home dir", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "")
		t.Setenv("HOME", "/home/tester")
		want := filepath.Join("/home/tester", ".local", "state", "acron")
		if got := StateDir(); got != want {
			t.Errorf("StateDir() = %q, want %q", got, want)
		}
	})
}

func TestSystemdUserDir(t *testing.T) {
	t.Run("XDG_CONFIG_HOME wins", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg/config")
		want := filepath.Join("/xdg/config", "systemd", "user")
		if got := SystemdUserDir(); got != want {
			t.Errorf("SystemdUserDir() = %q, want %q", got, want)
		}
	})
	t.Run("falls back to home dir", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/tester")
		want := filepath.Join("/home/tester", ".config", "systemd", "user")
		if got := SystemdUserDir(); got != want {
			t.Errorf("SystemdUserDir() = %q, want %q", got, want)
		}
	})
}

func TestExpandHome(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare tilde", "~", "/home/tester"},
		{"tilde slash", "~/projects/foo", filepath.Join("/home/tester", "projects", "foo")},
		{"absolute path unchanged", "/var/tmp", "/var/tmp"},
		{"relative path unchanged", "projects/foo", "projects/foo"},
		{"other-user tilde not expanded", "~otheruser/foo", "~otheruser/foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandHome(tt.in); got != tt.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("unresolvable home returns path unchanged", func(t *testing.T) {
		t.Setenv("HOME", "")
		if got := ExpandHome("~/foo"); got != "~/foo" {
			t.Errorf("ExpandHome(%q) = %q, want %q", "~/foo", got, "~/foo")
		}
	})
}

func TestPlistJobName(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantJob  string
		wantOk   bool
	}{
		{"acron plist", "com.acron.nightly-triage.plist", "nightly-triage", true},
		{"foreign app", "com.other.app.plist", "", false},
		{"missing .plist suffix", "com.acron.nightly-triage", "", false},
		{"prefix collision without separator", "com.acronsomething.plist", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, ok := PlistJobName(tt.filename)
			if ok != tt.wantOk {
				t.Fatalf("PlistJobName(%q) ok = %v, want %v", tt.filename, ok, tt.wantOk)
			}
			if ok && job != tt.wantJob {
				t.Errorf("PlistJobName(%q) job = %q, want %q", tt.filename, job, tt.wantJob)
			}
		})
	}
}

func TestPlistLabelJobNameRoundTrip(t *testing.T) {
	job := "nightly-triage"
	got, ok := PlistJobName(PlistLabel(job) + ".plist")
	if !ok || got != job {
		t.Errorf("round trip of %q = (%q, %v), want (%q, true)", job, got, ok, job)
	}
}
