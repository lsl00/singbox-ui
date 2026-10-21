package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// drawServerOverlay draws the details of an opened instance over the
// current page.
//
// The overlay is modal: it shows the connections and the log records of
// the instance while the page below keeps showing the local kernel.
func drawServerOverlay(screen tcell.Screen, state *app.App) {
	if state.OpenedServer < 0 {
		return
	}
	width, height := screen.Size()

	// The overlay is modal, so the page below is cleared first. Without
	// that the borders of the page and of the overlay would overlap.
	contentTop := 3
	contentBottom := height - 4
	if contentBottom <= contentTop {
		return
	}
	fillRect(screen, 0, contentTop, width, contentBottom-contentTop, cellStyle(colorText, colorBackground))

	boxWidth := min(max(width-4, 40), width)
	boxHeight := min(max(contentBottom-contentTop-2, 12), contentBottom-contentTop)
	boxX := max((width-boxWidth)/2, 0)
	boxY := max(contentTop+(contentBottom-contentTop-boxHeight)/2, contentTop)

	drawBox(screen, boxX, boxY, boxWidth, boxHeight, state.OverlayTitle())

	inner := boxWidth - 4
	if inner <= 0 || boxHeight < 6 {
		return
	}
	contentX := boxX + 2
	contentY := boxY + 1
	contentHeight := boxHeight - 2

	// The connections take the upper half, the log records the lower one.
	connHeight := contentHeight * 55 / 100
	if connHeight < 4 {
		connHeight = 4
	}
	logHeight := contentHeight - connHeight - 1
	if logHeight < 2 {
		logHeight = 2
	}

	drawOverlayConnections(screen, contentX, contentY, inner, connHeight, state)
	drawText(screen, contentX, contentY+connHeight, inner, strings.Repeat("-", inner), cellStyle(colorBorder, colorPanel))
	drawOverlayLogs(screen, contentX, contentY+connHeight+1, inner, logHeight, state)
}

// drawOverlayConnections renders the connection table of the overlay.
func drawOverlayConnections(screen tcell.Screen, x, y, width, height int, state *app.App) {
	connections := state.ServerConnections()
	up, down := int64(0), int64(0)
	for _, connection := range connections {
		up += connection.Upload
		down += connection.Download
	}
	title := fmt.Sprintf("Connections: %d active  UP %s  DN %s", len(connections), formatBytes(up), formatBytes(down))
	drawText(screen, x, y, width, title, cellStyle(colorText, colorPanel).Bold(true))
	if height < 3 {
		return
	}

	columns := connectionColumns(width)
	headers := []string{"Destination", "Proto", "Chain", "Rule", "Upload", "Download"}
	drawColumns(screen, x, y+1, columns, headers, cellStyle(colorMuted, colorPanel).Bold(true))

	visible := max(height-2, 1)
	start := 0
	if state.OverlayCursor() >= visible {
		start = state.OverlayCursor() - visible + 1
	}
	for line := 0; line < visible && start+line < len(connections); line++ {
		index := start + line
		connection := connections[index]
		values := []string{
			connection.Destination,
			connection.Protocol,
			joinChain(connection.Chain),
			connection.Rule,
			formatBytes(connection.Upload),
			formatBytes(connection.Download),
		}
		style := cellStyle(colorText, colorPanel)
		if index == state.OverlayCursor() {
			fillRect(screen, x-1, y+2+line, width+2, 1, selectedStyle())
			style = selectedStyle()
		}
		drawColumns(screen, x, y+2+line, columns, values, style)
	}
	if len(connections) == 0 {
		drawText(screen, x, y+2, width, "No connections.", cellStyle(colorMuted, colorPanel))
	}
}

// drawOverlayLogs renders the log records of the overlay.
func drawOverlayLogs(screen tcell.Screen, x, y, width, height int, state *app.App) {
	logs := state.ServerFilteredLogs()
	filter := state.LogLevelName()
	if filter != "all" {
		filter += "+"
	}
	title := fmt.Sprintf("Logs: %d shown, filter %s", len(logs), filter)
	if state.ServerLogFollow() {
		title += " [follow]"
	}
	drawText(screen, x, y, width, title, cellStyle(colorText, colorPanel).Bold(true))
	if height < 2 {
		return
	}

	visible := max(height-1, 1)
	start := logStart(len(logs), visible, state.ServerLogFollow(), state.ServerLogOffset())
	for line := 0; line < visible && start+line < len(logs); line++ {
		entry := logs[start+line]
		text := fmt.Sprintf("%s [%-7s] %s", entry.Time.Format("15:04:05"),
			strings.ToUpper(entry.Level), strings.ReplaceAll(entry.Text, "\n", " "))
		drawText(screen, x, y+1+line, width, text, cellStyle(logColor(entry.Level), colorPanel))
	}
	if len(logs) == 0 {
		drawText(screen, x, y+1, width, "No log records.", cellStyle(colorMuted, colorPanel))
	}
}
