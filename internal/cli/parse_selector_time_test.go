package cli

import (
	"testing"
	"time"
)

// parseSelectorTime backs `acron logs <job> <timestamp>`, so a regression that
// dropped a layout or the .log suffix would silently break log selection.
func TestParseSelectorTime(t *testing.T) {
	want := time.Date(2026, 6, 22, 14, 3, 0, 0, time.Local)

	accepted := []struct {
		name      string
		timestamp string
	}{
		{"display form", "2026-06-22 14:03:00"},
		{"log-filename form", "2026-06-22T14-03-00"},
		{"log-filename form with .log suffix", "2026-06-22T14-03-00.log"},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseSelectorTime(tt.timestamp)
			if !ok {
				t.Fatalf("parseSelectorTime(%q) ok = false, want true", tt.timestamp)
			}
			if !got.Equal(want) {
				t.Errorf("parseSelectorTime(%q) = %v, want %v", tt.timestamp, got, want)
			}
		})
	}

	rejected := []struct {
		name      string
		timestamp string
	}{
		{"date only", "2026-06-22"},
		{"garbage", "not-a-date"},
		{"empty", ""},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := parseSelectorTime(tt.timestamp); ok {
				t.Fatalf("parseSelectorTime(%q) ok = true, want false", tt.timestamp)
			}
		})
	}
}
