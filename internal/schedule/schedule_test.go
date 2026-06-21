package schedule

import "testing"

func ptr(n int) *int { return &n }

func eq(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestToLaunchd(t *testing.T) {
	tests := []struct {
		expr string
		want CalendarInterval
	}{
		{"0 2 * * *", CalendarInterval{Minute: ptr(0), Hour: ptr(2)}},
		{"30 14 1 6 *", CalendarInterval{Minute: ptr(30), Hour: ptr(14), Day: ptr(1), Month: ptr(6)}},
		{"0 9 * * 1", CalendarInterval{Minute: ptr(0), Hour: ptr(9), Weekday: ptr(1)}},
		{"* * * * *", CalendarInterval{}},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := ToLaunchd(tt.expr)
			if err != nil {
				t.Fatalf("ToLaunchd(%q): %v", tt.expr, err)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 interval, got %d", len(got))
			}
			ci := got[0]
			if !eq(ci.Minute, tt.want.Minute) || !eq(ci.Hour, tt.want.Hour) ||
				!eq(ci.Day, tt.want.Day) || !eq(ci.Weekday, tt.want.Weekday) ||
				!eq(ci.Month, tt.want.Month) {
				t.Errorf("ToLaunchd(%q) = %+v, want %+v", tt.expr, ci, tt.want)
			}
		})
	}
}

func TestToSystemd(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"0 2 * * *", "*-*-* 02:00:00"},
		{"30 14 1 6 *", "*-06-01 14:30:00"},
		{"0 9 * * 1", "Mon *-*-* 09:00:00"},
		{"0 0 * * 0", "Sun *-*-* 00:00:00"},
		{"* * * * *", "*-*-* *:*:00"},
		{"* 2 * * *", "*-*-* 02:*:00"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := ToSystemd(tt.expr)
			if err != nil {
				t.Fatalf("ToSystemd(%q): %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("ToSystemd(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestToSystemdRejects(t *testing.T) {
	tests := []string{
		"0 2 * *",      // too few fields
		"*/15 * * * *", // step
		"0 9 * * 1-5",  // range
		"0 0,12 * * *", // list
		"99 2 * * *",   // out of range
		"x 2 * * *",    // not a number
	}
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			if _, err := ToSystemd(expr); err == nil {
				t.Errorf("ToSystemd(%q): expected error, got nil", expr)
			}
		})
	}
}

func TestToLaunchdRejects(t *testing.T) {
	tests := []string{
		"0 2 * *",      // too few fields
		"*/15 * * * *", // step
		"0 9 * * 1-5",  // range
		"0 0,12 * * *", // list
		"99 2 * * *",   // out of range
		"x 2 * * *",    // not a number
	}
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			if _, err := ToLaunchd(expr); err == nil {
				t.Errorf("ToLaunchd(%q): expected error, got nil", expr)
			}
		})
	}
}
