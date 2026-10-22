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
		detailsHeight = detailPanelHeight
	}
	tableHeight := height - detailsHeight
	if tableHeight < 4 {
		tableHeight = height
		detailsHeight = 0
	}

	connections := state.Connections
	up, down := int64(0), int64(0)
	for _, connection := range connections {
		up += connection.Upload
		down += connection.Download
	}
	title := fmt.Sprintf("Connections: %d active  UP %s  DN %s  [sorted by %s]",
		len(connections), formatBytes(up), formatBytes(down), state.ConnSort.Label())
	drawBox(screen, x, y, width, tableHeight, title)
	if tableHeight < 3 {
		return
	}

	columns := connectionColumns(width)
	headers := []string{"Destination", "Proto", "Chain", "Rule", "Upload", "Download", "Age"}
	drawColumns(screen, x+1, y+1, columns, headers, cellStyle(colorMuted, colorBackground).Bold(true))

	visible := max(tableHeight-3, 1)
	start := 0
	if state.ConnCursor >= visible {
		start = state.ConnCursor - visible + 1
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
			formatAge(time.Since(connection.CreatedAt).Milliseconds()),
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
		lines := connectionDetailLines(connection)
		for index, line := range lines {
			if detailY+1+index >= detailY+detailsHeight-1 {
				break
			}
			drawText(screen, x+2, detailY+1+index, width-4, line, cellStyle(colorText, colorBackground))
		}
	}
}

// connectionDetailLines renders the lines of the detail panel of a
// connection.
//
// The connections of the local kernel and of a monitored instance share
// the panel, so both use the same lines.
func connectionDetailLines(connection app.ConnInfo) []string {
	inbound := valueOrDash(connection.Inbound)
	if connection.InboundType != "" {
		inbound += " (" + connection.InboundType + ")"
	}
	lines := []string{
		fmt.Sprintf("%s %s -> %s", valueOrDash(connection.Protocol), valueOrDash(connection.Source), valueOrDash(connection.Destination)),
		fmt.Sprintf("network: %s   dns: %s   inbound: %s", valueOrDash(connection.Network), valueOrDash(connection.DNSMode), inbound),
		fmt.Sprintf("rule: %s (%s)", valueOrDash(connection.Rule), valueOrDash(connection.RulePayload)),
		"chain: " + joinChain(connection.Chain),
		fmt.Sprintf("upload: %s   download: %s", formatBytes(connection.Upload), formatBytes(connection.Download)),
		fmt.Sprintf("started: %s", connection.CreatedAt.Format(time.RFC3339)),
	}
	if connection.Process != "" {
		lines = append(lines, "process: "+connection.Process)
	}
	return append(lines, "id: "+connection.ID)
}

// compactConnectionDetailLines renders the short form of the detail
// panel of a connection.
//
// A small box cannot hold every line of the panel.
func compactConnectionDetailLines(connection app.ConnInfo) []string {
	inbound := valueOrDash(connection.Inbound)
	if connection.InboundType != "" {
		inbound += " (" + connection.InboundType + ")"
	}
	return []string{
		fmt.Sprintf("%s %s -> %s", valueOrDash(connection.Protocol), valueOrDash(connection.Source), valueOrDash(connection.Destination)),
		fmt.Sprintf("network: %s   inbound: %s", valueOrDash(connection.Network), inbound),
		fmt.Sprintf("rule: %s   chain: %s", valueOrDash(connection.Rule), joinChain(connection.Chain)),
		fmt.Sprintf("upload: %s   download: %s", formatBytes(connection.Upload), formatBytes(connection.Download)),
		"id: " + connection.ID,
	}
}

// connectionColumns computes the column widths of the connection table.
func connectionColumns(width int) []int {
	base := []int{22, 8, 20, 14, 9, 9, 7}
	available := max(width-2, len(base))
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
