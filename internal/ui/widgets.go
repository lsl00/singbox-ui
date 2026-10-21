package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/gdamore/tcell/v2"
)

var (
	colorBackground = tcell.NewRGBColor(24, 24, 37)
	colorPanel      = tcell.NewRGBColor(30, 30, 46)
	colorBorder     = tcell.NewRGBColor(69, 71, 90)
	colorText       = tcell.NewRGBColor(205, 214, 244)
	colorMuted      = tcell.NewRGBColor(147, 153, 178)
	colorAccent     = tcell.NewRGBColor(137, 180, 250)
	colorGreen      = tcell.NewRGBColor(166, 227, 161)
	colorYellow     = tcell.NewRGBColor(249, 226, 175)
	colorRed        = tcell.NewRGBColor(243, 139, 168)
	colorPurple     = tcell.NewRGBColor(203, 166, 247)
)

func cellStyle(foreground, background tcell.Color) tcell.Style {
	return tcell.StyleDefault.Foreground(foreground).Background(background)
}

func selectedStyle() tcell.Style {
	return cellStyle(colorBackground, colorAccent).Bold(true)
}

// fillRect paints a rectangle, clipped to the screen.
func fillRect(screen tcell.Screen, x, y, width, height int, style tcell.Style) {
	if width <= 0 || height <= 0 {
		return
	}
	screenWidth, screenHeight := screen.Size()
	if x < 0 {
		width += x
		x = 0
	}
	if y < 0 {
		height += y
		y = 0
	}
	if x >= screenWidth || y >= screenHeight {
		return
	}
	width = minInt(width, screenWidth-x)
	height = minInt(height, screenHeight-y)
	if width <= 0 || height <= 0 {
		return
	}
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			screen.SetContent(x+column, y+row, ' ', nil, style)
		}
	}
}

// drawText writes text at the position, clipped to the given width.
func drawText(screen tcell.Screen, x, y, width int, text string, style tcell.Style) {
	if width <= 0 || y < 0 {
		return
	}
	screenWidth, screenHeight := screen.Size()
	if y >= screenHeight || x >= screenWidth {
		return
	}
	if x < 0 {
		runes := []rune(text)
		skip := minInt(-x, len(runes))
		text = string(runes[skip:])
		width += x
		x = 0
	}
	width = minInt(width, screenWidth-x)
	if width <= 0 {
		return
	}
	runes := []rune(text)
	if len(runes) > width {
		runes = runes[:width]
	}
	for index, value := range runes {
		screen.SetContent(x+index, y, value, nil, style)
	}
}

// drawBox draws a framed box with an optional title.
func drawBox(screen tcell.Screen, x, y, width, height int, title string) {
	if width <= 0 || height <= 0 {
		return
	}
	fillRect(screen, x, y, width, height, cellStyle(colorText, colorBackground))
	if width == 1 || height == 1 {
		return
	}
	style := cellStyle(colorBorder, colorBackground)
	for column := 0; column < width; column++ {
		screen.SetContent(x+column, y, '-', nil, style)
		screen.SetContent(x+column, y+height-1, '-', nil, style)
	}
	for row := 0; row < height; row++ {
		screen.SetContent(x, y+row, '|', nil, style)
		screen.SetContent(x+width-1, y+row, '|', nil, style)
	}
	screen.SetContent(x, y, '+', nil, style)
	screen.SetContent(x+width-1, y, '+', nil, style)
	screen.SetContent(x, y+height-1, '+', nil, style)
	screen.SetContent(x+width-1, y+height-1, '+', nil, style)
	if title != "" && width > 4 {
		drawText(screen, x+2, y, width-4, " "+truncate(title, width-6)+" ", cellStyle(colorAccent, colorBackground).Bold(true))
	}
}

// drawPlaceholder shows a centered hint box.
func drawPlaceholder(screen tcell.Screen, x, y, width, height int, title, detail string) {
	boxWidth := minInt(maxInt(width-4, 24), 64)
	boxHeight := minInt(maxInt(height-2, 5), 7)
	boxWidth = minInt(boxWidth, width)
	boxHeight = minInt(boxHeight, height)
	if boxWidth <= 0 || boxHeight <= 0 {
		return
	}
	boxX := x + maxInt((width-boxWidth)/2, 0)
	boxY := y + maxInt((height-boxHeight)/2, 0)
	drawBox(screen, boxX, boxY, boxWidth, boxHeight, title)
	if boxHeight >= 3 {
		drawText(screen, boxX+2, boxY+2, boxWidth-4, detail, cellStyle(colorMuted, colorBackground))
	}
	if boxHeight >= 5 {
		drawText(screen, boxX+2, boxY+4, boxWidth-4, "Press 5 for Settings or ? for help.", cellStyle(colorYellow, colorBackground))
	}
}

// splitWidths divides a width into count columns.
func splitWidths(total, count int) []int {
	if count <= 0 {
		return nil
	}
	widths := make([]int, count)
	base := total / count
	remainder := total % count
	for index := range widths {
		widths[index] = base
		if index < remainder {
			widths[index]++
		}
	}
	return widths
}

// drawColumns lays out values in fixed width columns.
func drawColumns(screen tcell.Screen, x, y int, widths []int, values []string, style tcell.Style) {
	position := x
	for index, width := range widths {
		if index >= len(values) {
			break
		}
		drawText(screen, position, y, width, truncate(values[index], width), style)
		position += width + 1
	}
}

// drawSparkline renders a bar chart of the given samples.
func drawSparkline(screen tcell.Screen, x, y, width int, label string, values []int64, peak int64, color tcell.Color) {
	if width <= 0 {
		return
	}
	drawText(screen, x, y, minInt(width, 4), label, cellStyle(color, colorBackground).Bold(true))
	graphX := x + 4
	graphWidth := width - 4
	if graphWidth <= 0 {
		return
	}
	if len(values) == 0 {
		drawText(screen, graphX, y, graphWidth, "no samples", cellStyle(colorMuted, colorBackground))
		return
	}
	if peak <= 0 {
		peak = 1
	}
	bars := " .:-=+*#%@"
	for column := 0; column < graphWidth; column++ {
		index := column * len(values) / graphWidth
		if index >= len(values) {
			index = len(values) - 1
		}
		ratio := float64(maxInt64(values[index], 0)) / float64(peak)
		level := int(math.Round(ratio * float64(len(bars)-1)))
		level = minInt(maxInt(level, 0), len(bars)-1)
		// A non zero sample must stay visible even when the other
		// direction dominates the shared scale.
		if level == 0 && values[index] > 0 {
			level = 1
		}
		screen.SetContent(graphX+column, y, rune(bars[level]), nil, cellStyle(color, colorBackground))
	}
}

// renderMetric draws a titled card with a single highlighted value.
func renderMetric(screen tcell.Screen, x, y, width, height int, title, value string, color tcell.Color) {
	drawBox(screen, x, y, width, height, title)
	if height >= 3 {
		drawText(screen, x+2, y+2, width-4, value, cellStyle(color, colorBackground).Bold(true))
	}
}

// renderStat draws a titled card with a muted value.
func renderStat(screen tcell.Screen, x, y, width, height int, title, value string) {
	drawBox(screen, x, y, width, height, title)
	if height >= 3 {
		drawText(screen, x+2, y+2, width-4, value, cellStyle(colorText, colorBackground))
	}
}

// truncate shortens a string and appends an ellipsis.
func truncate(value string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

// formatBytes renders a byte count with binary units.
func formatBytes(value int64) string {
	if value <= 0 {
		return "0 B"
	}
	scaled := float64(value)
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	unit := 0
	for scaled >= 1024 && unit < len(units)-1 {
		scaled /= 1024
		unit++
	}
	precision := 2
	if scaled >= 100 {
		precision = 0
	} else if scaled >= 10 {
		precision = 1
	}
	return fmt.Sprintf("%.*f %s", precision, scaled, units[unit])
}

// formatRate renders a byte rate.
func formatRate(value int64) string {
	return formatBytes(maxInt64(value, 0)) + "/s"
}

// formatDuration renders a millisecond duration in a compact form.
func formatDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "0s"
	}
	seconds := milliseconds / 1000
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if seconds < 3600 {
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dd %dh", hours/24, hours%24)
}

// formatAge renders a duration in the compact form used by the tables.
func formatAge(milliseconds int64) string {
	if milliseconds <= 0 {
		return "0s"
	}
	seconds := milliseconds / 1000
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm", seconds/60)
	case seconds < 86400:
		return fmt.Sprintf("%dh%dm", seconds/3600, (seconds%3600)/60)
	default:
		return fmt.Sprintf("%dd", seconds/86400)
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func joinChain(chain []string) string {
	if len(chain) == 0 {
		return "-"
	}
	return strings.Join(chain, " -> ")
}
