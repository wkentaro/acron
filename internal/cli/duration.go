package cli

import (
	"fmt"
	"time"
)

// formatDuration renders a non-negative duration in systemctl list-timers style:
// the largest non-zero unit followed by the next-smaller unit when it is
// non-zero, on a ladder capped at days (s, min, h, day/days). Sub-minute
// durations render as bare seconds. Seconds-truncating, so 1h35min42s reads
// "1h 35min". The day unit pluralizes ("1 day" vs "2 days"); s/min/h do not, and
// only the day unit carries a space, matching systemd.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int64(d / (24 * time.Hour))
	hours := int64(d % (24 * time.Hour) / time.Hour)
	minutes := int64(d % time.Hour / time.Minute)
	seconds := int64(d % time.Minute / time.Second)

	switch {
	case days > 0:
		day := fmt.Sprintf("%d days", days)
		if days == 1 {
			day = "1 day"
		}
		return appendUnit(day, hours, "h")
	case hours > 0:
		return appendUnit(fmt.Sprintf("%dh", hours), minutes, "min")
	case minutes > 0:
		return appendUnit(fmt.Sprintf("%dmin", minutes), seconds, "s")
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func appendUnit(head string, n int64, label string) string {
	if n == 0 {
		return head
	}
	return fmt.Sprintf("%s %d%s", head, n, label)
}
