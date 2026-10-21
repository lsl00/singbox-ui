package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// drawOverview renders the traffic, memory and connection overview.
func drawOverview(screen tcell.Screen, x, y, width, height int, state *app.App) {
	if !state.Connected && !state.TrafficUp && !state.MemoryUp {
		drawPlaceholder(screen, x, y, width, height, "Not connected",
			"Configure the controller URL on Settings, or start with --url URL.")
		return
	}
	if width < 8 || height < 5 {
		return
	}

	title := "Overview"
	drawText(screen, x+1, y, width-2, title, cellStyle(colorText, colorBackground).Bold(true))
	offset := x + 1 + len([]rune(title))
	drawText(screen, offset, y, width-(offset-x), "  mode ", cellStyle(colorMuted, colorBackground))
	offset += len([]rune("  mode "))
	drawText(screen, offset, y, width-(offset-x), state.Mode(), cellStyle(colorAccent, colorBackground).Bold(true))
	offset += len([]rune(state.Mode()))
	if state.Runtime != nil && state.Runtime.LogLevel != "" {
		drawText(screen, offset, y, width-(offset-x), "  log "+state.Runtime.LogLevel, cellStyle(colorMuted, colorBackground))
	}
	if !state.StreamUp["traffic"] {
		drawText(screen, x+width-14, y, 13, "[reconnecting]", cellStyle(colorYellow, colorBackground))
	}

	cardsY := y + 2
	cardsHeight := 5
	if cardsY+cardsHeight > y+height {
		return
	}
	cardWidths := splitWidths(width, 4)
	cardX := x
	traffic := state.Traffic
	renderMetric(screen, cardX, cardsY, cardWidths[0], cardsHeight, "Uplink rate", formatRate(traffic.Up), colorGreen)
	cardX += cardWidths[0]
	renderMetric(screen, cardX, cardsY, cardWidths[1], cardsHeight, "Downlink rate", formatRate(traffic.Down), colorAccent)
	cardX += cardWidths[1]
	renderMetric(screen, cardX, cardsY, cardWidths[2], cardsHeight, "Total upload", formatBytes(traffic.UpTotal), colorText)
	cardX += cardWidths[2]
	renderMetric(screen, cardX, cardsY, cardWidths[3], cardsHeight, "Total download", formatBytes(traffic.DownTotal), colorText)

	lowerY := cardsY + cardsHeight + 1
	if lowerY >= y+height {
		return
	}
	statsHeight := 4
	bandHeight := y + height - lowerY - statsHeight - 1
	if bandHeight < 4 {
		bandHeight = max(y+height-lowerY, 4)
		statsHeight = 0
	}
	if bandHeight > 0 {
		drawBandwidth(screen, x, lowerY, width, bandHeight, state)
	}
	if statsHeight > 0 && lowerY+bandHeight+1 < y+height {
		statsY := lowerY + bandHeight + 1
		statWidths := splitWidths(width, 4)
		statX := x
		memory := "-"
		if state.MemoryUp {
			memory = formatBytes(int64(state.Memory))
		}
		values := []string{
			memory,
			fmt.Sprintf("%d", len(state.Connections)),
			state.Mode(),
			logLevelName(state),
		}
		labels := []string{"Kernel memory", "Connections", "Mode", "Log level"}
		for index := range statWidths {
			renderStat(screen, statX, statsY, statWidths[index], statsHeight, labels[index], values[index])
			statX += statWidths[index]
		}
	}
}

// drawBandwidth renders the rate history of the last samples.
func drawBandwidth(screen tcell.Screen, x, y, width, height int, state *app.App) {
	drawBox(screen, x, y, width, height, "Bandwidth")
	if width < 10 || height < 4 {
		return
	}
	peak := int64(0)
	for _, value := range state.HistoryUp {
		peak = max(peak, value)
	}
	for _, value := range state.HistoryDown {
		peak = max(peak, value)
	}
	drawText(screen, x+2, y+1, width-4, "peak "+formatRate(peak), cellStyle(colorMuted, colorBackground))
	drawSparkline(screen, x+2, y+2, width-4, "UP", state.HistoryUp, peak, colorGreen)
	if height >= 5 {
		drawSparkline(screen, x+2, y+3, width-4, "DN", state.HistoryDown, peak, colorAccent)
	}
}

// logLevelName returns the log level reported by the kernel.
func logLevelName(state *app.App) string {
	if state.Runtime == nil || state.Runtime.LogLevel == "" {
		return "-"
	}
	return state.Runtime.LogLevel
}
