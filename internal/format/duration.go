package format

import (
	"fmt"
	"time"
)

// TimeAgo returns a human-friendly relative time string like "5m ago".
func TimeAgo(t time.Time) string {
	return DurationAgo(time.Since(t))
}

// DurationAgo formats a duration as a human-friendly "ago" string.
func DurationAgo(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}
