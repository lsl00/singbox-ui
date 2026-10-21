package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
	"mihomo-tui/internal/config"
)

// drawServers renders the grid of monitored instances.
func drawServers(screen tcell.Screen, x, y, width, height int, state *app.App) {
	if len(state.Servers) == 0 {
		drawPlaceholder(screen, x, y, width, height, "No servers configured",
			"Start with --servers PATH to monitor instances.")
		return
	}
	columns, cardWidth, cardHeight := serverGridColumns(width)
	rowsVisible := max(height/cardHeight, 1)
	state.ServersPageSize = rowsVisible * columns
	cursorRow := state.ServerCursor / columns
	if cursorRow < state.ServerScroll {
		state.ServerScroll = cursorRow
	}
	if cursorRow >= state.ServerScroll+rowsVisible {
		state.ServerScroll = cursorRow - rowsVisible + 1
	}
	for row := 0; row < rowsVisible; row++ {
		for column := 0; column < columns; column++ {
			index := (state.ServerScroll+row)*columns + column
			if index >= len(state.Servers) {
				return
			}
			drawServerCard(screen, x+column*cardWidth, y+row*cardHeight, cardWidth, cardHeight,
				&state.Servers[index], index == state.ServerCursor)
		}
	}
}

// serverGridColumns computes the card layout of the servers page.
func serverGridColumns(width int) (int, int, int) {
	const (
		minCardWidth = 30
		cardHeight   = 7
	)
	columns := max(width/minCardWidth, 1)
	return columns, width / columns, cardHeight
}

// drawServerCard renders one monitored instance.
func drawServerCard(screen tcell.Screen, x, y, width, height int, monitor *app.ServerMonitor, selected bool) {
	title := monitor.Entry.Name
	titleColor := colorYellow
	switch {
	case monitor.Err != "":
		titleColor = colorRed
	case monitor.Connected:
		titleColor = colorGreen
	}
	if selected {
		title = "> " + title
	}
	drawBox(screen, x, y, width, height, "")
	drawText(screen, x+2, y, width-4, " "+truncate(title, width-6)+" ",
		cellStyle(titleColor, colorBackground).Bold(true))
	if height < 4 {
		return
	}
	if monitor.Info != nil {
		info := monitor.Info
		drawText(screen, x+2, y+2, width-4,
			fmt.Sprintf("%s %s", protocolLabel(monitor.Entry.Protocol), info.Version),
			cellStyle(colorMuted, colorBackground))
		drawText(screen, x+2, y+3, width-4,
			fmt.Sprintf("UP %-12s %s", formatRate(info.Uplink), formatBytes(info.UpTotal)),
			cellStyle(colorGreen, colorBackground))
		drawText(screen, x+2, y+4, width-4,
			fmt.Sprintf("DN %-12s %s", formatRate(info.Downlink), formatBytes(info.DownTotal)),
			cellStyle(colorAccent, colorBackground))
		if height >= 6 {
			stats := fmt.Sprintf("mem %-10s conns %-4d go %d",
				formatBytes(int64(info.Memory)), info.Conns, info.Goroutines)
			// The goroutine count is the first thing that goes when the
			// card is too narrow for it.
			if len([]rune(stats)) > width-4 {
				stats = fmt.Sprintf("mem %-10s conns %d",
					formatBytes(int64(info.Memory)), info.Conns)
			}
			drawText(screen, x+2, y+5, width-4, stats, cellStyle(colorMuted, colorBackground))
		}
		return
	}
	state := "connecting..."
	stateColor := colorYellow
	if monitor.Err != "" {
		state = truncate("offline: "+monitor.Err, max(width-4, 0))
		stateColor = colorRed
	}
	drawText(screen, x+2, y+2, width-4, state, cellStyle(stateColor, colorBackground))
}

// protocolLabel renders the API of a monitored instance.
func protocolLabel(protocol config.ServerProtocol) string {
	if protocol == config.ProtocolClash {
		return "mihomo"
	}
	return "sing-box"
}
