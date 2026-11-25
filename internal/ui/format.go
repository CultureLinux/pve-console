package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// statusDot returns the glyph and the style that show a guest status.
func statusDot(status string) (string, lipgloss.Style) {
	switch status {
	case "running":
		return glyphDotOn, styleOK
	case "stopped":
		return glyphDotOff, styleMuted
	}
	return glyphDotOther, styleWarn
}

// ratio is used/total as a value between 0 and 1, or 0 when total is unknown.
func ratio(used float64, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return used / total
}

// percent formats a ratio as a 4-column percentage such as " 12%".
func percent(r float64) string {
	return fmt.Sprintf("%3.0f%%", r*100)
}

// right pads text on the left to width columns, to align numbers.
func right(text string, width int) string {
	if w := textWidth(text); w < width {
		return strings.Repeat(" ", width-w) + text
	}
	return text
}

func durationLabel(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %02dh", days, hours)
	}
	return fmt.Sprintf("%02dh%02dm", hours, int(d.Minutes())%60)
}

func timeLabel(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}
	return time.Unix(timestamp, 0).Format("2006-01-02 15:04")
}

func bytesPair(used uint64, total uint64) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s", humanBytes(used), humanBytes(total))
}

func humanBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div := uint64(unit)
	exp := 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}

// truncate shortens text to limit columns, with an ellipsis.
func truncate(value string, limit int) string {
	return ellipsis(value, limit)
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
