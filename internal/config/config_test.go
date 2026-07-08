package config

import (
	"testing"
	"time"
)

func validJob(cwd string) Job {
	return Job{
		Name:     "nightly-triage",
		Schedule: "0 2 * * *",
		Agent:    []string{"claude", "-p", "{prompt}"},
		Prompt:   "Triage open issues",
		Cwd:      cwd,
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		mutate  func(*Job)
		wantErr bool
	}{
		{"valid", nil, false},
		{"missing name", func(j *Job) { j.Name = "" }, true},
		{"bad name", func(j *Job) { j.Name = "Nightly Triage" }, true},
		{"missing schedule", func(j *Job) { j.Schedule = "" }, true},
		{"bad schedule", func(j *Job) { j.Schedule = "not cron" }, true},
		{"empty agent", func(j *Job) { j.Agent = nil }, true},
		{"empty agent command", func(j *Job) { j.Agent = []string{""} }, true},
		{"missing prompt", func(j *Job) { j.Prompt = "" }, true},
		{"missing cwd", func(j *Job) { j.Cwd = "" }, true},
		{"nonexistent cwd", func(j *Job) { j.Cwd = dir + "/nope" }, true},
		{"relative cwd", func(j *Job) { j.Cwd = "." }, true},
		{"bad timeout", func(j *Job) { j.Timeout = "soon" }, true},
		{"negative timeout", func(j *Job) { j.Timeout = "-5m" }, true},
		{"zero timeout opts out", func(j *Job) { j.Timeout = "0" }, false},
		{"valid condition", func(j *Job) { j.Condition = []string{"sh", "-c", "true"} }, false},
		{"empty condition command", func(j *Job) { j.Condition = []string{""} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := validJob(dir)
			if tt.mutate != nil {
				tt.mutate(&job)
			}
			err := (&Config{Jobs: []Job{job}}).Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateRejectsDuplicateName(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Jobs: []Job{validJob(dir), validJob(dir)}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func TestResolvedTimeout(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", time.Hour},
		{"0", 0},
		{"3h", 3 * time.Hour},
	}
	for _, tt := range tests {
		got, err := Job{Timeout: tt.in}.ResolvedTimeout()
		if err != nil {
			t.Fatalf("ResolvedTimeout(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("ResolvedTimeout(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestNextFire(t *testing.T) {
	after := time.Date(2026, 6, 22, 23, 21, 0, 0, time.UTC)
	got, err := Job{Schedule: "*/20 * * * *"}.NextFire(after)
	if err != nil {
		t.Fatalf("NextFire: %v", err)
	}
	want := time.Date(2026, 6, 22, 23, 40, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("NextFire = %v, want %v", got, want)
	}
}

func TestNextFireRejectsBadSchedule(t *testing.T) {
	if _, err := (Job{Schedule: "nonsense"}).NextFire(time.Now()); err == nil {
		t.Error("expected error for unparseable schedule")
	}
}

func TestIsEnabledDefaultsTrue(t *testing.T) {
	if !(Job{}).IsEnabled() {
		t.Error("unset enabled should default to true")
	}
	disabled := false
	if (Job{Enabled: &disabled}).IsEnabled() {
		t.Error("enabled=false should report disabled")
	}
}

func TestJobLookup(t *testing.T) {
	cfg := &Config{Jobs: []Job{{Name: "alpha"}, {Name: "beta"}}}

	job, err := cfg.Job("beta")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Name != "beta" {
		t.Errorf("Job(\"beta\").Name = %q, want %q", job.Name, "beta")
	}

	if _, err := cfg.Job("missing"); err == nil {
		t.Error("expected error for absent job")
	}
}
