// Package format renders sizes, durations and counts for humans.
package format

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Size formats a byte count with binary units, e.g. "1.2 MiB".
func Size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Int formats n with thousands separators.
func Int(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Duration formats seconds as MM:SS or H:MM:SS; "Unknown" when not positive.
func Duration(sec float64) string {
	if !(sec > 0) {
		return "Unknown"
	}
	t := int(sec + 0.5)
	h, m, s := t/3600, t/60%60, t%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// Bitrate formats bits per second; "Unknown" when not positive.
func Bitrate(bps int64) string {
	switch {
	case bps <= 0:
		return "Unknown"
	case bps >= 1_000_000:
		return fmt.Sprintf("%.1f Mb/s", float64(bps)/1e6)
	default:
		return fmt.Sprintf("%d kb/s", bps/1000)
	}
}

// Elapsed formats a duration for humans: "850 ms", "12.4 s", "2m 31s", "1h 02m".
func Elapsed(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%d ms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1f s", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}
