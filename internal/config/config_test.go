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
		{"missing prompt", func(j *Job) { j.Prompt = "" }, true},
		{"missing cwd", func(j *Job) { j.Cwd = "" }, true},
		{"nonexistent cwd", func(j *Job) { j.Cwd = dir + "/nope" }, true},
		{"bad timeout", func(j *Job) { j.Timeout = "soon" }, true},
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

func TestIsEnabledDefaultsTrue(t *testing.T) {
	if !(Job{}).IsEnabled() {
		t.Error("unset enabled should default to true")
	}
	disabled := false
	if (Job{Enabled: &disabled}).IsEnabled() {
		t.Error("enabled=false should report disabled")
	}
}
