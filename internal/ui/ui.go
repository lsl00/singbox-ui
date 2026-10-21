package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// Draw renders the whole dashboard.
func Draw(screen tcell.Screen, state *app.App) {
	width, height := screen.Size()
	screen.SetStyle(cellStyle(colorText, colorBackground))
	screen.Clear()
	if width < 1 || height < 1 {
		return
	}

	drawHeader(screen, 0, 0, width, state)
	footerY := height - 3
	mainHeight := footerY - 3
	if mainHeight > 0 {
		switch state.View {
		case app.ViewOverview:
			drawOverview(screen, 0, 3, width, mainHeight, state)
		case app.ViewGroups:
			drawGroups(screen, 0, 3, width, mainHeight, state)
		case app.ViewConnections:
			drawConnections(screen, 0, 3, width, mainHeight, state)
		case app.ViewLogs:
			drawLogs(screen, 0, 3, width, mainHeight, state)
		case app.ViewSettings:
			drawSettings(screen, 0, 3, width, mainHeight, state)
		case app.ViewServers:
			drawServers(screen, 0, 3, width, mainHeight, state)
		}
	}
	if footerY >= 0 {
		drawFooter(screen, 0, footerY, width, state)
	}
	if state.Help {
		drawHelp(screen, width, height)
	}
	screen.Show()
}

// drawHeader renders the title bar and the page tabs.
func drawHeader(screen tcell.Screen, x, y, width int, state *app.App) {
	fillRect(screen, x, y, width, 3, cellStyle(colorText, colorPanel))
	version := "mihomo"
	if state.Version != nil && state.Version.Version != "" {
		version = "v" + state.Version.Version
	}
	statusText, statusColor := "[offline]", colorRed
	switch {
	case state.Connecting:
		statusText, statusColor = "[connecting]", colorYellow
	case state.Connected:
		statusText, statusColor = "[connected]", colorGreen
	}
	drawText(screen, x+1, y, width-2, " mihomo TUI ", cellStyle(colorText, colorPanel).Bold(true))
	drawText(screen, x+13, y, width-14, version, cellStyle(colorMuted, colorPanel))
	offset := 13 + len([]rune(version)) + 2
	drawText(screen, x+offset, y, width-offset-1, statusText, cellStyle(statusColor, colorPanel))

	tabX := x + 1
	for view := app.ViewOverview; view < app.ViewCount; view++ {
		label := fmt.Sprintf("[%d] %s", view.Number(), view.Title())
		style := cellStyle(colorMuted, colorPanel)
		if state.View == view {
			style = style.Foreground(colorAccent).Bold(true)
		}
		drawText(screen, tabX, y+1, width-(tabX-x)-1, label, style)
		tabX += len([]rune(label)) + 2
		if tabX >= x+width {
			break
		}
	}
	drawText(screen, x, y+2, width, strings.Repeat("-", maxInt(width, 0)), cellStyle(colorBorder, colorPanel))
}

// drawFooter renders the key hints and the status message.
func drawFooter(screen tcell.Screen, x, y, width int, state *app.App) {
	fillRect(screen, x, y, width, 3, cellStyle(colorText, colorPanel))
	drawText(screen, x, y, width, strings.Repeat("-", maxInt(width, 0)), cellStyle(colorBorder, colorPanel))
	hints := "1-6 pages  r refresh  ? help  q quit"
	switch state.View {
	case app.ViewOverview:
		hints = "[/] or m switch mode  r refresh  1-6 pages"
	case app.ViewGroups:
		hints = "Up/Down select  Enter switch  t test  T test group  x expand"
	case app.ViewConnections:
		hints = "Up/Down select  Enter details  c close  C close all  s sort"
	case app.ViewLogs:
		hints = "Up/Down scroll  PgUp/PgDn  f filter  a follow  c clear"
	case app.ViewSettings:
		hints = "Up/Down field  Enter/e edit  c connect  d disconnect  R reload  X restart"
	case app.ViewServers:
		hints = "Up/Down select  PgUp/PgDn page  Home/End"
	}
	drawText(screen, x+1, y+1, width-2, hints, cellStyle(colorMuted, colorPanel))

	message := "ready"
	messageColor := colorMuted
	if state.Error != "" {
		message, messageColor = state.Error, colorRed
	} else if state.Message != "" {
		message, messageColor = state.Message, colorYellow
	}
	drawText(screen, x+1, y+2, width-2, truncate(message, maxInt(width-2, 0)), cellStyle(messageColor, colorPanel))
}

// drawHelp renders the keyboard shortcut overlay.
func drawHelp(screen tcell.Screen, width, height int) {
	boxWidth := minInt(maxInt(width-4, 30), 72)
	boxHeight := minInt(maxInt(height-4, 12), 18)
	boxWidth = minInt(boxWidth, width)
	boxHeight = minInt(boxHeight, height)
	if boxWidth <= 0 || boxHeight <= 0 {
		return
	}
	boxX := maxInt((width-boxWidth)/2, 0)
	boxY := maxInt((height-boxHeight)/2, 0)
	fillRect(screen, boxX, boxY, boxWidth, boxHeight, cellStyle(colorText, colorPanel))
	drawBox(screen, boxX, boxY, boxWidth, boxHeight, "Help")
	lines := []string{
		"1..6       switch between pages",
		"Tab        next page",
		"r          request all data now",
		"q / Ctrl-C quit",
		"",
		"Overview:  [ ] m   switch rule/global/direct",
		"Groups:    Up/Down, Enter, t, T, x",
		"Connections: Up/Down, Enter, c, C, s",
		"Logs:      Up/Down, PgUp/PgDn, Home/End, f, a, c",
		"Settings:  Up/Down, Enter/e, c, d, R, X, F, P",
		"Servers:   Up/Down, PgUp/PgDn, Home/End",
		"",
		"Press ? or Esc to close.",
	}
	for index, line := range lines {
		if boxY+2+index >= boxY+boxHeight-1 {
			break
		}
		color := colorText
		if strings.HasPrefix(line, "Press ") {
			color = colorYellow
		}
		drawText(screen, boxX+2, boxY+2+index, boxWidth-4, line, cellStyle(color, colorPanel))
	}
}
