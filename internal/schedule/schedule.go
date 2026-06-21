package schedule

import (
	"fmt"
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

// ToLaunchd translates a 5-field cron expression with calendar semantics into
// launchd StartCalendarInterval match points.
//
// Lists, ranges, and steps (",", "-", "/") are not supported yet; they require
// enumerating into multiple match dicts and will be added later.
func ToLaunchd(expr string) ([]CalendarInterval, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("schedule %q: expected 5 cron fields, got %d", expr, len(fields))
	}

	var ci CalendarInterval
	specs := []struct {
		name     string
		field    string
		dst      **int
		min, max int
	}{
		{"minute", fields[0], &ci.Minute, 0, 59},
		{"hour", fields[1], &ci.Hour, 0, 23},
		{"day-of-month", fields[2], &ci.Day, 1, 31},
		{"month", fields[3], &ci.Month, 1, 12},
		{"day-of-week", fields[4], &ci.Weekday, 0, 6},
	}
	for _, spec := range specs {
		value, err := parseField(spec.field, spec.min, spec.max)
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %s: %w", expr, spec.name, err)
		}
		*spec.dst = value
	}
	return []CalendarInterval{ci}, nil
}

func parseField(field string, min, max int) (*int, error) {
	if field == "*" {
		return nil, nil
	}
	if strings.ContainsAny(field, ",-/") {
		return nil, fmt.Errorf("lists, ranges, and steps are not supported yet (got %q)", field)
	}
	n, err := strconv.Atoi(field)
	if err != nil {
		return nil, fmt.Errorf("invalid value %q", field)
	}
	if n < min || n > max {
		return nil, fmt.Errorf("value %d out of range [%d,%d]", n, min, max)
	}
	return &n, nil
}
