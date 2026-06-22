package schedule_test

import (
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/wkentaro/acron/internal/config"
	"github.com/wkentaro/acron/internal/schedule"
)

// robfigAccepts probes the validation/display authority (config.scheduleParser,
// reached through Job.NextFire) for whether it accepts an expression.
func robfigAccepts(expr string) bool {
	_, err := config.Job{Schedule: expr}.NextFire(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return err == nil
}

// TestTranslatorsAgreeWithAuthority locks the accepted-syntax sets together: an
// expression that Config.Validate's parser accepts must translate via both
// ToSystemd and ToLaunchd, and one it rejects must be rejected by both. This is
// what guarantees no config validates yet fails to install (or vice versa).
func TestTranslatorsAgreeWithAuthority(t *testing.T) {
	exprs := []string{
		// Plain numeric forms.
		"0 2 * * *",
		"30 14 1 6 *",
		"0 9 * * 1",
		"* * * * *",
		"*/15 * * * *",
		"0 9 * * 1-5",
		"0 9 * * 6",  // max weekday accepted
		"0 0 13 * 5", // DOM/DOW OR; both paths apply POSIX OR here.
		// Named months and weekdays, including case variants and ranges.
		"0 9 * * MON",
		"0 9 * * mon",
		"0 9 * * Mon-Fri",
		"30 14 1 JUN *",
		"0 9 * JAN-MAR *",
		"0 0 13 * FRI",
		// "?" as the robfig alias for "*", in every field (robfig is field-agnostic).
		"0 9 ? * *",
		"0 9 * * ?",
		"? * * * *",
		"0 ? * * *",
		"0 0 * ? *",
		// "*"/"?" as a range low bound spans the whole range; robfig ignores the
		// high bound, so neither restricts. Both positions (DOM and DOW) must be
		// treated as unrestricted for the POSIX OR rule.
		"0 0 * * ?-5",
		"0 0 * * *-5",
		"0 0 *-5 * *",
		"0 0 ?-5 * 1",
		// Rejected by the authority; both translators must reject too.
		"0 2 * *",      // too few fields
		"99 2 * * *",   // out of range
		"x 2 * * *",    // not a number
		"*/0 * * * *",  // zero step
		"17-9 * * * *", // descending range
		"0 0 * * 7",    // weekday out of range
		"0 0 JAN * *",  // month name in day-of-month field
		"0 0 * MON *",  // weekday name in month field
		"0 0 * * JANK", // unknown name
		"@daily",       // descriptor not enabled on the authority
	}
	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			want := robfigAccepts(expr)
			_, systemdErr := schedule.ToSystemd(expr)
			_, launchdErr := schedule.ToLaunchd(expr)
			if got := systemdErr == nil; got != want {
				t.Errorf("ToSystemd(%q) accepted=%v, authority accepted=%v (err=%v)", expr, got, want, systemdErr)
			}
			if got := launchdErr == nil; got != want {
				t.Errorf("ToLaunchd(%q) accepted=%v, authority accepted=%v (err=%v)", expr, got, want, launchdErr)
			}
		})
	}
}

// TestEveryNameRendersLikeItsNumber pins the translator's month/weekday name
// tables to robfig's: every canonical short name robfig accepts must translate,
// rendering identically to the numeric form of the same field. A future robfig
// alias the translator has not mirrored then surfaces here rather than as a
// config that validates yet fails to install.
func TestEveryNameRendersLikeItsNumber(t *testing.T) {
	months := []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}
	for i, name := range months {
		t.Run(name, func(t *testing.T) {
			assertNameRendersLikeNumber(t, "0 0 1 "+name+" *", "0 0 1 "+strconv.Itoa(i+1)+" *")
		})
	}

	weekdays := []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}
	for num, name := range weekdays {
		t.Run(name, func(t *testing.T) {
			assertNameRendersLikeNumber(t, "0 0 * * "+name, "0 0 * * "+strconv.Itoa(num))
		})
	}
}

func assertNameRendersLikeNumber(t *testing.T, named, numeric string) {
	t.Helper()
	if !robfigAccepts(named) {
		t.Errorf("authority rejected %q", named)
	}

	gotSystemd, err := schedule.ToSystemd(named)
	if err != nil {
		t.Fatalf("ToSystemd(%q): %v", named, err)
	}
	wantSystemd, err := schedule.ToSystemd(numeric)
	if err != nil {
		t.Fatalf("ToSystemd(%q): %v", numeric, err)
	}
	if !slices.Equal(gotSystemd, wantSystemd) {
		t.Errorf("ToSystemd(%q) = %v, want same as %q: %v", named, gotSystemd, numeric, wantSystemd)
	}

	gotLaunchd, err := schedule.ToLaunchd(named)
	if err != nil {
		t.Fatalf("ToLaunchd(%q): %v", named, err)
	}
	wantLaunchd, err := schedule.ToLaunchd(numeric)
	if err != nil {
		t.Fatalf("ToLaunchd(%q): %v", numeric, err)
	}
	if !reflect.DeepEqual(gotLaunchd, wantLaunchd) {
		t.Errorf("ToLaunchd(%q) = %+v, want same as %q: %+v", named, gotLaunchd, numeric, wantLaunchd)
	}
}
