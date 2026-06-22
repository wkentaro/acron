package schedule

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CalendarInterval is a launchd StartCalendarInterval match point. A nil field
// means "every" (the field is omitted, so launchd matches any value).
type CalendarInterval struct {
	Minute  *int
	Hour    *int
	Day     *int
	Weekday *int
	Month   *int
}

// fields holds the expanded value set for each cron field. A nil slice means
// "every" (a bare "*"); a populated slice is the sorted, unique set of matches.
type fields struct {
	minute  []int
	hour    []int
	day     []int
	month   []int
	weekday []int
	// domDowOr is set when both day-of-month and day-of-week are restricted, so
	// POSIX cron fires when either matches. The two backends express this OR by
	// emitting two match points instead of one.
	domDowOr bool
}

// ToLaunchd translates a 5-field cron expression with calendar semantics into
// launchd StartCalendarInterval match points. Lists, ranges, and steps expand
// into the cartesian product of their per-field values, one match dict each.
func ToLaunchd(expr string) ([]CalendarInterval, error) {
	f, err := parse(expr)
	if err != nil {
		return nil, err
	}
	return f.intervals()
}

// ToSystemd translates a 5-field cron expression into systemd OnCalendar values
// (`DOW *-MM-DD HH:MM:00`). Lists, ranges, and steps render as systemd's native
// comma-separated field syntax. A combined day-of-month + day-of-week schedule
// yields two values (a date-only and a weekday-only line); systemd unions
// multiple OnCalendar= lines, which gives the POSIX OR semantics. Every
// returned value must be emitted as its own OnCalendar= directive.
func ToSystemd(expr string) ([]string, error) {
	f, err := parse(expr)
	if err != nil {
		return nil, err
	}
	return f.onCalendar(), nil
}

func parse(expr string) (fields, error) {
	cronFields := strings.Fields(expr)
	if len(cronFields) != 5 {
		return fields{}, fmt.Errorf("schedule %q: expected 5 cron fields, got %d", expr, len(cronFields))
	}

	// monthNames and weekdayNames mirror the case-insensitive aliases the robfig
	// parser accepts for the month and day-of-week fields, so any schedule that
	// Config.Validate accepts also translates here.
	monthNames := map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}
	weekdayNames := map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}

	var f fields
	specs := []struct {
		name     string
		field    string
		dst      *[]int
		min, max int
		names    map[string]int
	}{
		{"minute", cronFields[0], &f.minute, 0, 59, nil},
		{"hour", cronFields[1], &f.hour, 0, 23, nil},
		{"day-of-month", cronFields[2], &f.day, 1, 31, nil},
		{"month", cronFields[3], &f.month, 1, 12, monthNames},
		{"day-of-week", cronFields[4], &f.weekday, 0, 6, weekdayNames},
	}
	for _, spec := range specs {
		values, err := parseField(spec.field, spec.min, spec.max, spec.names)
		if err != nil {
			return fields{}, fmt.Errorf("schedule %q: %s: %w", expr, spec.name, err)
		}
		*spec.dst = values
	}
	// POSIX cron ORs day-of-month and day-of-week when both are restricted. A
	// field starting with "*" (e.g. "*/2") or a "?" (the robfig alias for "*")
	// is unrestricted here even though it may parse to a populated slice, so the
	// check reads the raw field rather than f.day/f.weekday.
	f.domDowOr = isRestricted(cronFields[2]) && isRestricted(cronFields[4])
	return f, nil
}

// isRestricted reports whether a day field narrows the schedule. A field opening
// with "*" or "?" matches every value (robfig sets its star bit), so it does
// not restrict; anything else does.
func isRestricted(field string) bool {
	return !strings.HasPrefix(field, "*") && !strings.HasPrefix(field, "?")
}

// domDowArm is one (day, weekday) pair to expand into match points.
type domDowArm struct{ day, weekday []int }

// domDowArms returns the (day, weekday) field pairs to expand. Normally that is
// the single pair as given, but when both day-of-month and day-of-week are
// restricted POSIX cron fires when either matches, which neither backend can
// express in one match point. The OR then becomes two arms, one matching by day
// (weekday omitted) and one by weekday (day omitted).
func (f fields) domDowArms() []domDowArm {
	if f.domDowOr {
		return []domDowArm{
			{day: f.day},
			{weekday: f.weekday},
		}
	}
	return []domDowArm{{day: f.day, weekday: f.weekday}}
}

// intervals builds the cartesian product of the per-field value sets, one
// launchd match dict per combination. Iteration is month-major, minute-minor;
// the order is deterministic and tests depend on it, though launchd itself does
// not care about dict order.
func (f fields) intervals() ([]CalendarInterval, error) {
	// maxLaunchdIntervals caps the expansion so a pathological schedule
	// (stepping every field) cannot generate a multi-megabyte plist; the
	// largest sane schedule is well under it. systemd needs no such cap since
	// it renders comma lists, not dicts.
	const maxLaunchdIntervals = 100000

	months := pointers(f.month)
	hours := pointers(f.hour)
	minutes := pointers(f.minute)

	type expandedArm struct{ days, weekdays []*int }
	rawArms := f.domDowArms()
	arms := make([]expandedArm, len(rawArms))
	for i, arm := range rawArms {
		arms[i] = expandedArm{pointers(arm.day), pointers(arm.weekday)}
	}

	count := 0
	for _, arm := range arms {
		count += len(months) * len(arm.days) * len(arm.weekdays) * len(hours) * len(minutes)
	}
	if count > maxLaunchdIntervals {
		return nil, fmt.Errorf("schedule expands to %d launchd entries, exceeding the limit of %d; use a coarser schedule", count, maxLaunchdIntervals)
	}

	out := make([]CalendarInterval, 0, count)
	for _, arm := range arms {
		for _, month := range months {
			for _, day := range arm.days {
				for _, weekday := range arm.weekdays {
					for _, hour := range hours {
						for _, minute := range minutes {
							out = append(out, CalendarInterval{
								Minute:  minute,
								Hour:    hour,
								Day:     day,
								Weekday: weekday,
								Month:   month,
							})
						}
					}
				}
			}
		}
	}
	return out, nil
}

// pointers turns an expanded value set into the pointers to iterate when
// building match dicts; an empty set yields a single nil ("every").
func pointers(values []int) []*int {
	if len(values) == 0 {
		return []*int{nil}
	}
	out := make([]*int, len(values))
	for i := range values {
		out[i] = &values[i]
	}
	return out
}

func (f fields) onCalendar() []string {
	arms := f.domDowArms()
	lines := make([]string, len(arms))
	for i, arm := range arms {
		lines[i] = f.onCalendarLine(arm.day, arm.weekday)
	}
	return lines
}

func (f fields) onCalendarLine(day, weekday []int) string {
	var b strings.Builder
	if len(weekday) > 0 {
		weekdayAbbrevs := [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
		names := make([]string, len(weekday))
		for i, wd := range weekday {
			names[i] = weekdayAbbrevs[wd]
		}
		b.WriteString(strings.Join(names, ","))
		b.WriteByte(' ')
	}
	fmt.Fprintf(&b, "*-%s-%s %s:%s:00",
		calendarField(f.month), calendarField(day),
		calendarField(f.hour), calendarField(f.minute))
	return b.String()
}

func calendarField(values []int) string {
	if len(values) == 0 {
		return "*"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%02d", v)
	}
	return strings.Join(parts, ",")
}

func parseField(field string, min, max int, names map[string]int) ([]int, error) {
	// A field opening with "*" or "?" without a "/" step is unrestricted: robfig
	// ignores any range high bound and treats the field as matching every value.
	if !strings.ContainsRune(field, '/') && (strings.HasPrefix(field, "*") || strings.HasPrefix(field, "?")) {
		return nil, nil
	}
	seen := map[int]bool{}
	for _, term := range strings.Split(field, ",") {
		values, err := parseTerm(term, min, max, names)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			seen[v] = true
		}
	}
	out := make([]int, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

func parseTerm(term string, min, max int, names map[string]int) ([]int, error) {
	spec, step := term, 0
	if slash := strings.IndexByte(term, '/'); slash >= 0 {
		spec = term[:slash]
		n, err := strconv.Atoi(term[slash+1:])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid step in %q", term)
		}
		step = n
	}

	lo, hi, err := parseRange(spec, min, max, step > 0, names)
	if err != nil {
		return nil, err
	}
	if step == 0 {
		step = 1
	}
	var values []int
	for v := lo; v <= hi; v += step {
		values = append(values, v)
	}
	return values, nil
}

// parseRange resolves the value part of a term (before any "/step") into an
// inclusive [lo, hi] bound. A bare number spans to max when a step follows
// (`5/10` means 5,15,...); otherwise it is the single value `5`.
func parseRange(spec string, min, max int, stepped bool, names map[string]int) (lo, hi int, err error) {
	switch {
	case spec == "*" || spec == "?":
		return min, max, nil
	case strings.ContainsRune(spec, '-'):
		bounds := strings.SplitN(spec, "-", 2)
		// A "*" or "?" low bound means the whole range; robfig ignores the high
		// bound entirely here, so neither restricts.
		if bounds[0] == "*" || bounds[0] == "?" {
			return min, max, nil
		}
		if lo, err = boundedAtoi(bounds[0], min, max, names); err != nil {
			return 0, 0, err
		}
		if hi, err = boundedAtoi(bounds[1], min, max, names); err != nil {
			return 0, 0, err
		}
		if lo > hi {
			return 0, 0, fmt.Errorf("range %q is descending", spec)
		}
		return lo, hi, nil
	default:
		if lo, err = boundedAtoi(spec, min, max, names); err != nil {
			return 0, 0, err
		}
		if stepped {
			return lo, max, nil
		}
		return lo, lo, nil
	}
}

func boundedAtoi(s string, min, max int, names map[string]int) (int, error) {
	n, ok := names[strings.ToLower(s)]
	if !ok {
		var err error
		if n, err = strconv.Atoi(s); err != nil {
			return 0, fmt.Errorf("invalid value %q", s)
		}
	}
	if n < min || n > max {
		return 0, fmt.Errorf("value %d out of range [%d,%d]", n, min, max)
	}
	return n, nil
}
