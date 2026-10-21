package ui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// drawConnections renders the table of tracked connections.
func drawConnections(screen tcell.Screen, x, y, width, height int, state *app.App) {
	detailsHeight := 0
	if state.ShowDetails {
		detailsHeight = 8
	}
	tableHeight := height - detailsHeight
	if tableHeight < 4 {
		tableHeight = height
		detailsHeight = 0
	}

	up, down := int64(0), int64(0)
	for _, connection := range state.Connections {
		up += connection.Upload
		down += connection.Download
	}
	title := fmt.Sprintf("Connections: %d active  UP %s  DN %s  [sorted by %s]",
		len(state.Connections), formatBytes(up), formatBytes(down), state.ConnSort.Label())
	drawBox(screen, x, y, width, tableHeight, title)
	if tableHeight < 3 {
		return
	}

	columns := connectionColumns(width)
	headers := []string{"Destination", "Proto", "Chain", "Rule", "Upload", "Download", "Age"}
	drawColumns(screen, x+1, y+1, columns, headers, cellStyle(colorMuted, colorBackground).Bold(true))

	visible := maxInt(tableHeight-3, 1)
	start := 0
	if state.ConnCursor >= visible {
		start = state.ConnCursor - visible + 1
	}
	for line := 0; line < visible && start+line < len(state.Connections); line++ {
		index := start + line
		connection := state.Connections[index]
		values := []string{
			connection.Destination(),
			connection.Metadata.Type,
			joinChain(connection.Chains),
			connection.Rule,
			formatBytes(connection.Upload),
			formatBytes(connection.Download),
			formatAge(time.Since(connection.Start).Milliseconds()),
		}
		style := cellStyle(colorText, colorBackground)
		if index == state.ConnCursor {
			fillRect(screen, x+1, y+2+line, width-2, 1, selectedStyle())
			style = selectedStyle()
		}
		drawColumns(screen, x+1, y+2+line, columns, values, style)
	}

	if detailsHeight > 0 {
		detailY := y + tableHeight
		drawBox(screen, x, detailY, width, detailsHeight, "Connection details")
		connection, ok := state.SelectedConnection()
		if !ok {
			drawText(screen, x+2, detailY+1, width-4, "No connection selected.", cellStyle(colorMuted, colorBackground))
			return
		}
		metadata := connection.Metadata
		destination := metadata.DestinationIP
		if metadata.Host != "" {
			destination = metadata.Host + " (" + metadata.DestinationIP + ")"
		}
		lines := []string{
			fmt.Sprintf("%s %s -> %s:%s", metadata.Type, connection.Source(), destination, metadata.DestinationPort),
			fmt.Sprintf("network: %s   dns: %s   inbound: %s", metadata.Network, valueOrDash(metadata.DNSMode), valueOrDash(metadata.InboundName)),
			fmt.Sprintf("rule: %s (%s)", valueOrDash(connection.Rule), valueOrDash(connection.RulePayload)),
			"chain: " + joinChain(connection.Chains),
			fmt.Sprintf("upload: %s   download: %s", formatBytes(connection.Upload), formatBytes(connection.Download)),
			fmt.Sprintf("started: %s", connection.Start.Format(time.RFC3339)),
			"id: " + connection.ID,
		}
		for index, line := range lines {
			if detailY+1+index >= detailY+detailsHeight-1 {
				break
			}
			drawText(screen, x+2, detailY+1+index, width-4, line, cellStyle(colorText, colorBackground))
		}
	}
}

// connectionColumns computes the column widths of the connection table.
func connectionColumns(width int) []int {
	base := []int{22, 8, 20, 14, 9, 9, 7}
	available := maxInt(width-2, len(base))
	need := 0
	for _, value := range base {
		need += value
	}
	need += len(base) - 1
	if need <= available {
		return base
	}
	shrink := need - available
	for index := len(base) - 1; index >= 0 && shrink > 0; index-- {
		minimum := 4
		canShrink := base[index] - minimum
		if canShrink > shrink {
			canShrink = shrink
		}
		base[index] -= canShrink
		shrink -= canShrink
	}
	return base
}
