package schedule

import "testing"

func ptr(n int) *int { return &n }

func eq(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func eqInterval(a, b CalendarInterval) bool {
	return eq(a.Minute, b.Minute) && eq(a.Hour, b.Hour) && eq(a.Day, b.Day) &&
		eq(a.Weekday, b.Weekday) && eq(a.Month, b.Month)
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
			if !eqInterval(got[0], tt.want) {
				t.Errorf("ToLaunchd(%q) = %+v, want %+v", tt.expr, got[0], tt.want)
			}
		})
	}
}

func TestToLaunchdExpands(t *testing.T) {
	tests := []struct {
		expr string
		want []CalendarInterval
	}{
		{"*/15 * * * *", []CalendarInterval{
			{Minute: ptr(0)}, {Minute: ptr(15)}, {Minute: ptr(30)}, {Minute: ptr(45)},
		}},
		{"0 0,12 * * *", []CalendarInterval{
			{Minute: ptr(0), Hour: ptr(0)}, {Minute: ptr(0), Hour: ptr(12)},
		}},
		{"0 9 * * 1-5", []CalendarInterval{
			{Minute: ptr(0), Hour: ptr(9), Weekday: ptr(1)},
			{Minute: ptr(0), Hour: ptr(9), Weekday: ptr(2)},
			{Minute: ptr(0), Hour: ptr(9), Weekday: ptr(3)},
			{Minute: ptr(0), Hour: ptr(9), Weekday: ptr(4)},
			{Minute: ptr(0), Hour: ptr(9), Weekday: ptr(5)},
		}},
		{"0,30 9-10 * * *", []CalendarInterval{
			{Minute: ptr(0), Hour: ptr(9)},
			{Minute: ptr(30), Hour: ptr(9)},
			{Minute: ptr(0), Hour: ptr(10)},
			{Minute: ptr(30), Hour: ptr(10)},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := ToLaunchd(tt.expr)
			if err != nil {
				t.Fatalf("ToLaunchd(%q): %v", tt.expr, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ToLaunchd(%q): got %d intervals, want %d", tt.expr, len(got), len(tt.want))
			}
			for i := range got {
				if !eqInterval(got[i], tt.want[i]) {
					t.Errorf("ToLaunchd(%q)[%d] = %+v, want %+v", tt.expr, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestToLaunchdRejectsExplosion(t *testing.T) {
	expr := "*/1 */1 */1 */1 */1" // expands past maxLaunchdIntervals
	if _, err := ToLaunchd(expr); err == nil {
		t.Errorf("ToLaunchd(%q): expected error for oversized expansion, got nil", expr)
	}
	if _, err := ToSystemd(expr); err != nil {
		t.Errorf("ToSystemd(%q): renders comma lists without enumerating, want no error, got %v", expr, err)
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
		{"*/15 * * * *", "*-*-* *:00,15,30,45:00"},
		{"0 0,12 * * *", "*-*-* 00,12:00:00"},
		{"0 9 * * 1-5", "Mon,Tue,Wed,Thu,Fri *-*-* 09:00:00"},
		{"0 9-12 * * *", "*-*-* 09,10,11,12:00:00"},
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
	for _, expr := range rejectCases() {
		t.Run(expr, func(t *testing.T) {
			if _, err := ToSystemd(expr); err == nil {
				t.Errorf("ToSystemd(%q): expected error, got nil", expr)
			}
		})
	}
}

func TestToLaunchdRejects(t *testing.T) {
	for _, expr := range rejectCases() {
		t.Run(expr, func(t *testing.T) {
			if _, err := ToLaunchd(expr); err == nil {
				t.Errorf("ToLaunchd(%q): expected error, got nil", expr)
			}
		})
	}
}

// A "*"-prefixed field (e.g. "*/2") is unrestricted for the POSIX OR rule, so
// pairing one with a restricted partner is AND, not the unsupported OR; both
// directions must still be accepted by both backends.
func TestStarPrefixedDayWithWeekdayAccepted(t *testing.T) {
	for _, expr := range []string{"0 9 */2 * 1", "0 9 15 * */2"} {
		t.Run(expr, func(t *testing.T) {
			if _, err := ToLaunchd(expr); err != nil {
				t.Errorf("ToLaunchd(%q): unexpected error: %v", expr, err)
			}
			if _, err := ToSystemd(expr); err != nil {
				t.Errorf("ToSystemd(%q): unexpected error: %v", expr, err)
			}
		})
	}
}

func rejectCases() []string {
	return []string{
		"0 2 * *",       // too few fields
		"99 2 * * *",    // out of range
		"x 2 * * *",     // not a number
		"*/0 * * * *",   // zero step
		"17-9 * * * *",  // descending range
		"60-70 2 * * *", // range out of bounds
		"0 9 15 * 1",    // both day-of-month and day-of-week set (POSIX OR, unsupported)
	}
}
