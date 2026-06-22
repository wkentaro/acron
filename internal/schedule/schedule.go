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

// ToSystemd translates a 5-field cron expression into a systemd OnCalendar
// value (`DOW *-MM-DD HH:MM:00`). Lists, ranges, and steps render as systemd's
// native comma-separated field syntax.
func ToSystemd(expr string) (string, error) {
	f, err := parse(expr)
	if err != nil {
		return "", err
	}
	return f.onCalendar(), nil
}

func parse(expr string) (fields, error) {
	cronFields := strings.Fields(expr)
	if len(cronFields) != 5 {
		return fields{}, fmt.Errorf("schedule %q: expected 5 cron fields, got %d", expr, len(cronFields))
	}

	var f fields
	specs := []struct {
		name     string
		field    string
		dst      *[]int
		min, max int
	}{
		{"minute", cronFields[0], &f.minute, 0, 59},
		{"hour", cronFields[1], &f.hour, 0, 23},
		{"day-of-month", cronFields[2], &f.day, 1, 31},
		{"month", cronFields[3], &f.month, 1, 12},
		{"day-of-week", cronFields[4], &f.weekday, 0, 6},
	}
	for _, spec := range specs {
		values, err := parseField(spec.field, spec.min, spec.max)
		if err != nil {
			return fields{}, fmt.Errorf("schedule %q: %s: %w", expr, spec.name, err)
		}
		*spec.dst = values
	}
	// POSIX cron ORs day-of-month and day-of-week when both are restricted,
	// which neither backend can represent. A field starting with "*" (e.g.
	// "*/2") is unrestricted here even though it parses to a populated slice,
	// so the check reads the raw field rather than f.day/f.weekday.
	domRestricted := !strings.HasPrefix(cronFields[2], "*")
	dowRestricted := !strings.HasPrefix(cronFields[4], "*")
	if domRestricted && dowRestricted {
		return fields{}, fmt.Errorf("schedule %q: combined day-of-month and day-of-week (POSIX OR) is not supported yet", expr)
	}
	return f, nil
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
	days := pointers(f.day)
	weekdays := pointers(f.weekday)
	hours := pointers(f.hour)
	minutes := pointers(f.minute)

	count := len(months) * len(days) * len(weekdays) * len(hours) * len(minutes)
	if count > maxLaunchdIntervals {
		return nil, fmt.Errorf("schedule expands to %d launchd entries, exceeding the limit of %d; use a coarser schedule", count, maxLaunchdIntervals)
	}

	out := make([]CalendarInterval, 0, count)
	for _, month := range months {
		for _, day := range days {
			for _, weekday := range weekdays {
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

func (f fields) onCalendar() string {
	var b strings.Builder
	if len(f.weekday) > 0 {
		weekdayNames := [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
		names := make([]string, len(f.weekday))
		for i, wd := range f.weekday {
			names[i] = weekdayNames[wd]
		}
		b.WriteString(strings.Join(names, ","))
		b.WriteByte(' ')
	}
	fmt.Fprintf(&b, "*-%s-%s %s:%s:00",
		calendarField(f.month), calendarField(f.day),
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

func parseField(field string, min, max int) ([]int, error) {
	if field == "*" {
		return nil, nil
	}
	seen := map[int]bool{}
	for _, term := range strings.Split(field, ",") {
		values, err := parseTerm(term, min, max)
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

func parseTerm(term string, min, max int) ([]int, error) {
	spec, step := term, 0
	if slash := strings.IndexByte(term, '/'); slash >= 0 {
		spec = term[:slash]
		n, err := strconv.Atoi(term[slash+1:])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid step in %q", term)
		}
		step = n
	}

	lo, hi, err := parseRange(spec, min, max, step > 0)
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
func parseRange(spec string, min, max int, stepped bool) (lo, hi int, err error) {
	switch {
	case spec == "*":
		return min, max, nil
	case strings.ContainsRune(spec, '-'):
		bounds := strings.SplitN(spec, "-", 2)
		if lo, err = boundedAtoi(bounds[0], min, max); err != nil {
			return 0, 0, err
		}
		if hi, err = boundedAtoi(bounds[1], min, max); err != nil {
			return 0, 0, err
		}
		if lo > hi {
			return 0, 0, fmt.Errorf("range %q is descending", spec)
		}
		return lo, hi, nil
	default:
		if lo, err = boundedAtoi(spec, min, max); err != nil {
			return 0, 0, err
		}
		if stepped {
			return lo, max, nil
		}
		return lo, lo, nil
	}
}

func boundedAtoi(s string, min, max int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", s)
	}
	if n < min || n > max {
		return 0, fmt.Errorf("value %d out of range [%d,%d]", n, min, max)
	}
	return n, nil
}
