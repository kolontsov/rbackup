package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var dayDurationRe = regexp.MustCompile(`^(\d+)d(.*)$`)

// parseDuration extends time.ParseDuration with support for "d" (days = 24h).
func parseDuration(s string) (time.Duration, error) {
	if m := dayDurationRe.FindStringSubmatch(s); m != nil {
		days, _ := strconv.Atoi(m[1])
		d := time.Duration(days) * 24 * time.Hour
		if m[2] != "" {
			rest, err := time.ParseDuration(m[2])
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", s, err)
			}
			d += rest
		}
		return d, nil
	}
	return time.ParseDuration(s)
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncateVersion(id string) string {
	if id == "" {
		return ""
	}
	if len(id) <= 8 {
		return id
	}
	return "..." + id[len(id)-8:]
}
