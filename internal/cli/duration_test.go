package cli

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"sub-minute", 12 * time.Second, "12s"},
		{"sub-minute truncates", 12*time.Second + 900*time.Millisecond, "12s"},
		{"minutes and seconds", 2*time.Minute + 34*time.Second, "2min 34s"},
		{"whole minute drops seconds", 5 * time.Minute, "5min"},
		{"hours and minutes", time.Hour + 35*time.Minute, "1h 35min"},
		{"hours drop seconds", time.Hour + 35*time.Minute + 42*time.Second, "1h 35min"},
		{"whole hour", 2 * time.Hour, "2h"},
		{"days and hours", 3*24*time.Hour + 4*time.Hour, "3 days 4h"},
		{"one day singular", 24 * time.Hour, "1 day"},
		{"one day plus hours", 24*time.Hour + 5*time.Hour, "1 day 5h"},
		{"zero hours drops lower units", 24*time.Hour + 45*time.Minute, "1 day"},
		{"multi day no hours", 45 * 24 * time.Hour, "45 days"},
		{"days never roll into weeks", 400 * 24 * time.Hour, "400 days"},
		{"negative clamps to zero", -5 * time.Second, "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatDuration(tc.d); got != tc.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
