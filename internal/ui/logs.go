package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// drawLogs renders the buffered kernel log.
func drawLogs(screen tcell.Screen, x, y, width, height int, state *app.App) {
	values := state.FilteredLogs()
	filter := state.LogLevelName()
	if filter != "all" {
		filter += "+"
	}
	title := fmt.Sprintf("Logs: %d shown, filter %s", len(values), filter)
	if state.LogFollow {
		title += " [follow]"
	}
	drawBox(screen, x, y, width, height, title)
	if height < 3 {
		return
	}
	visible := max(height-2, 1)
	start := logStart(len(values), visible, state.LogFollow, state.LogOffset)
	for line := 0; line < visible && start+line < len(values); line++ {
		entry := values[start+line]
		color := logColor(entry.Level)
		text := fmt.Sprintf("%s [%-7s] %s", entry.Time.Format("15:04:05"), strings.ToUpper(entry.Level),
			strings.ReplaceAll(entry.Text, "\n", " "))
		drawText(screen, x+2, y+1+line, width-4, text, cellStyle(color, colorBackground))
	}
	if len(values) == 0 {
		drawText(screen, x+2, y+1, width-4, "No log entries.", cellStyle(colorMuted, colorBackground))
	}
}

// logColor maps a log level to a color.
func logColor(level string) tcell.Color {
	switch strings.ToLower(level) {
	case "debug":
		return colorPurple
	case "info":
		return colorText
	case "warning":
		return colorYellow
	case "error":
		return colorRed
	default:
		return colorMuted
	}
}

// logStart computes the first visible line of the log view.
func logStart(total, height int, follow bool, offset int) int {
	if total <= height {
		return 0
	}
	if follow {
		return total - height
	}
	if offset == int(^uint(0)>>1) {
		return 0
	}
	return max(total-offset-height, 0)
}
