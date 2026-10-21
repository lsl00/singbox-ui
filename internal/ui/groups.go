package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// drawGroups renders the policy group tree.
func drawGroups(screen tcell.Screen, x, y, width, height int, state *app.App) {
	modeHeight := 0
	if state.Runtime != nil {
		modeHeight = 3
		drawBox(screen, x, y, width, modeHeight, "Mode")
		line := ""
		for _, mode := range []string{"rule", "global", "direct"} {
			if mode == state.Mode() {
				line += "[" + mode + "] "
			} else {
				line += " " + mode + "  "
			}
		}
		drawText(screen, x+2, y+1, width-4, line+"[/] or m", cellStyle(colorText, colorBackground))
	}
	listY := y + modeHeight
	listHeight := height - modeHeight
	if listHeight <= 0 {
		return
	}

	rows := state.Rows()
	drawBox(screen, x, listY, width, listHeight, fmt.Sprintf("Proxy Groups (%d)", len(state.GroupNames)))
	if listHeight < 3 {
		return
	}
	visible := max(listHeight-2, 1)
	start := 0
	if state.GroupCursor >= visible {
		start = state.GroupCursor - visible + 1
	}
	for lineIndex := 0; lineIndex < visible && start+lineIndex < len(rows); lineIndex++ {
		rowIndex := start + lineIndex
		row := rows[rowIndex]
		style := cellStyle(colorText, colorBackground)
		if rowIndex == state.GroupCursor {
			fillRect(screen, x+1, listY+1+lineIndex, width-2, 1, selectedStyle())
			style = selectedStyle()
		}
		if row.GroupIndex >= len(state.GroupNames) {
			continue
		}
		group := state.Proxies[state.GroupNames[row.GroupIndex]]
		if !row.IsItem {
			marker := "[+]"
			if state.GroupExpanded(group.Name) {
				marker = "[-]"
			}
			selected := "auto"
			if group.Now != "" {
				selected = "now: " + group.Now
			}
			drawText(screen, x+2, listY+1+lineIndex, width-4,
				fmt.Sprintf("%s %-28s  %-12s  %s", marker, truncate(group.Name, 28), truncate(group.Type, 12), selected), style)
			continue
		}
		if row.ItemIndex >= len(group.All) {
			continue
		}
		node := group.All[row.ItemIndex]
		itemStyle := style
		if rowIndex != state.GroupCursor && node == group.Now {
			itemStyle = cellStyle(colorGreen, colorBackground)
		}
		marker := " "
		if node == group.Now {
			marker = "*"
		}
		proxy := state.Proxies[node]
		delay, known := proxy.LastDelay()
		drawText(screen, x+2, listY+1+lineIndex, width-4,
			fmt.Sprintf("  %s %-32s  %-12s  %8s", marker, truncate(node, 32), truncate(proxy.Type, 12), formatDelay(delay, known)), itemStyle)
	}
	if len(rows) == 0 {
		drawText(screen, x+2, listY+1, width-4, "No proxy groups.", cellStyle(colorMuted, colorBackground))
	}
}
