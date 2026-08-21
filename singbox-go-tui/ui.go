package main

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

func drawScreen(screen tcell.Screen, app *App) {
	width, height := screen.Size()
	screen.SetStyle(cellStyle(colorText, colorBackground))
	screen.Clear()
	if width < 1 || height < 1 {
		return
	}

	drawHeader(screen, 0, 0, width, app)
	footerY := height - 3
	mainHeight := footerY - 3
	if mainHeight > 0 {
		switch app.view {
		case ViewOverview:
			drawOverview(screen, 0, 3, width, mainHeight, app)
		case ViewGroups:
			drawGroups(screen, 0, 3, width, mainHeight, app)
		case ViewConnections:
			drawConnections(screen, 0, 3, width, mainHeight, app)
		case ViewLogs:
			drawLogs(screen, 0, 3, width, mainHeight, app)
		case ViewSettings:
			drawSettings(screen, 0, 3, width, mainHeight, app)
		case ViewServers:
			drawServers(screen, 0, 3, width, mainHeight, app)
		}
	}
	if footerY >= 0 {
		drawFooter(screen, 0, footerY, width, app)
	}
	if app.help {
		drawHelp(screen, width, height)
	}
	screen.Show()
}

func cellStyle(foreground, background tcell.Color) tcell.Style {
	return tcell.StyleDefault.Foreground(foreground).Background(background)
}

func selectedStyle() tcell.Style {
	return cellStyle(colorBackground, colorAccent).Bold(true)
}

func drawHeader(screen tcell.Screen, x, y, width int, app *App) {
	fillRect(screen, x, y, width, 3, cellStyle(colorText, colorPanel))
	version := "sing-box"
	if app.version != nil && app.version.Version != "" {
		version += " " + app.version.Version
	}
	statusText, statusColor := "[offline]", colorRed
	if app.connecting {
		statusText, statusColor = "[connecting]", colorYellow
	} else if app.connected {
		statusText, statusColor = "[connected]", colorGreen
	}
	drawText(screen, x+1, y, width-2, " sing-box TUI ", cellStyle(colorText, colorPanel).Bold(true))
	drawText(screen, x+17, y, width-18, version, cellStyle(colorMuted, colorPanel))
	drawText(screen, x+17+len([]rune(version))+2, y, width-20-len([]rune(version)), statusText, cellStyle(statusColor, colorPanel))

	tabX := x + 1
	for view := ViewOverview; view <= ViewServers; view++ {
		label := fmt.Sprintf("[%d] %s", view.number(), view.title())
		style := cellStyle(colorMuted, colorPanel)
		if app.view == view {
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

func drawFooter(screen tcell.Screen, x, y, width int, app *App) {
	fillRect(screen, x, y, width, 3, cellStyle(colorText, colorPanel))
	drawText(screen, x, y, width, strings.Repeat("-", maxInt(width, 0)), cellStyle(colorBorder, colorPanel))
	hints := "1-5 pages  r refresh  ? help  q quit"
	switch app.view {
	case ViewGroups:
		hints = "Up/Down select  Enter switch  t test  x expand  [/]/m mode"
	case ViewConnections:
		hints = "Up/Down select  Enter details  c close  C close all"
	case ViewLogs:
		hints = "Up/Down scroll  PgUp/PgDn  f filter  a follow  c clear"
	case ViewSettings:
		hints = "Up/Down field  Enter/e edit  c connect  d disconnect"
	case ViewServers:
		hints = "Up/Down select  PgUp/PgDn page  Home/End"
	}
	drawText(screen, x+1, y+1, width-2, hints, cellStyle(colorMuted, colorPanel))
	message := "ready"
	messageColor := colorMuted
	if app.error != "" {
		message, messageColor = app.error, colorRed
	} else if app.message != "" {
		message, messageColor = app.message, colorYellow
	}
	// The first footer row is intentionally kept for the key hints. Status is
	// drawn on the last row so errors remain visible without a popup.
	drawText(screen, x+1, y+2, width-2, truncate(message, maxInt(width-2, 0)), cellStyle(messageColor, colorPanel))
}

func drawOverview(screen tcell.Screen, x, y, width, height int, app *App) {
	if !app.connected && app.status == nil {
		drawPlaceholder(screen, x, y, width, height, "Not connected", "Configure the API on Settings, or start with --url URL.")
		return
	}
	if width < 8 || height < 5 {
		return
	}
	status := app.status
	service := "unknown"
	serviceColor := colorMuted
	if app.service != nil {
		service = serviceStatusLabel(*app.service)
		switch app.service.Status {
		case 2:
			serviceColor = colorGreen
		case 1, 3:
			serviceColor = colorYellow
		case 4:
			serviceColor = colorRed
		}
	}
	uptime := "uptime -"
	if app.hasStarted {
		uptime = "uptime " + formatDuration(nowMillis()-app.startedAt)
	}
	drawText(screen, x+1, y, width-2, "Overview  "+uptime+"  service "+service, cellStyle(colorText, colorBackground).Bold(true))
	// Repaint the service part with its state color when it fits.
	serviceX := x + 1 + len([]rune("Overview  "+uptime+"  service "))
	drawText(screen, serviceX, y, width-(serviceX-x), service, cellStyle(serviceColor, colorBackground))

	cardsY := y + 2
	cardsHeight := 5
	if cardsY+cardsHeight > y+height {
		return
	}
	cardWidths := splitWidths(width, 4)
	cardX := x
	uplink, downlink, uplinkTotal, downlinkTotal := int64(0), int64(0), int64(0), int64(0)
	if status != nil {
		uplink, downlink = status.Uplink, status.Downlink
		uplinkTotal, downlinkTotal = status.UplinkTotal, status.DownlinkTotal
	}
	renderMetric(screen, cardX, cardsY, cardWidths[0], cardsHeight, "Uplink rate", formatRate(uplink), colorGreen)
	cardX += cardWidths[0]
	renderMetric(screen, cardX, cardsY, cardWidths[1], cardsHeight, "Downlink rate", formatRate(downlink), colorAccent)
	cardX += cardWidths[1]
	renderMetric(screen, cardX, cardsY, cardWidths[2], cardsHeight, "Total uplink", formatBytes(uplinkTotal), colorText)
	cardX += cardWidths[2]
	renderMetric(screen, cardX, cardsY, cardWidths[3], cardsHeight, "Total downlink", formatBytes(downlinkTotal), colorText)

	lowerY := cardsY + cardsHeight + 1
	if lowerY >= y+height {
		return
	}
	statsHeight := 4
	bandHeight := y + height - lowerY - statsHeight - 1
	if bandHeight < 4 {
		bandHeight = maxInt(y+height-lowerY, 4)
		statsHeight = 0
	}
	if bandHeight > 0 {
		drawBandwidth(screen, x, lowerY, width, bandHeight, app)
	}
	if statsHeight > 0 && lowerY+bandHeight+1 < y+height {
		statsY := lowerY + bandHeight + 1
		statWidths := splitWidths(width, 4)
		statX := x
		values := []string{"-", "-", "-", "-"}
		if status != nil {
			values = []string{
				formatBytes(int64(status.Memory)),
				fmt.Sprintf("%d", status.Goroutines),
				fmt.Sprintf("%d", status.ConnectionsIn),
				fmt.Sprintf("%d", status.ConnectionsOut),
			}
		}
		labels := []string{"Memory", "Goroutines", "Inbound connections", "Outbound connections"}
		for index := range statWidths {
			renderStat(screen, statX, statsY, statWidths[index], statsHeight, labels[index], values[index])
			statX += statWidths[index]
		}
	}
}

func drawBandwidth(screen tcell.Screen, x, y, width, height int, app *App) {
	drawBox(screen, x, y, width, height, "Bandwidth")
	if width < 10 || height < 4 {
		return
	}
	peak := int64(0)
	for _, value := range app.historyUp {
		peak = maxInt64(peak, value)
	}
	for _, value := range app.historyDown {
		peak = maxInt64(peak, value)
	}
	drawText(screen, x+2, y+1, width-4, "peak "+formatRate(peak), cellStyle(colorMuted, colorBackground))
	drawSparkline(screen, x+2, y+2, width-4, "UP", app.historyUp, peak, colorGreen)
	if height >= 5 {
		drawSparkline(screen, x+2, y+3, width-4, "DN", app.historyDown, peak, colorAccent)
	}
}

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
		screen.SetContent(graphX+column, y, rune(bars[level]), nil, cellStyle(color, colorBackground))
	}
}

func drawGroups(screen tcell.Screen, x, y, width, height int, app *App) {
	modeHeight := 0
	if app.clashMode != nil && len(app.clashMode.ModeList) > 0 {
		modeHeight = 3
		drawBox(screen, x, y, width, modeHeight, "Clash mode")
		line := ""
		for _, mode := range app.clashMode.ModeList {
			if mode == app.clashMode.CurrentMode {
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
	rows := app.groupRows()
	drawBox(screen, x, listY, width, listHeight, fmt.Sprintf("Proxy Groups (%d)", len(rows)))
	visible := maxInt(listHeight-2, 1)
	start := 0
	if app.groupCursor >= visible {
		start = app.groupCursor - visible + 1
	}
	for lineIndex := 0; lineIndex < visible && start+lineIndex < len(rows); lineIndex++ {
		rowIndex := start + lineIndex
		row := rows[rowIndex]
		style := cellStyle(colorText, colorBackground)
		if rowIndex == app.groupCursor {
			fillRect(screen, x+1, listY+1+lineIndex, width-2, 1, selectedStyle())
			style = selectedStyle()
		}
		if !row.IsItem {
			group := app.groups[row.GroupIndex]
			marker := "[+]"
			if group.IsExpand {
				marker = "[-]"
			}
			selected := "readonly"
			if group.Selectable {
				selected = "selected: " + valueOrDash(group.Selected)
			}
			drawText(screen, x+2, listY+1+lineIndex, width-4,
				fmt.Sprintf("%s %-30s  %-10s  %s", marker, truncate(group.Tag, 30), truncate(group.GroupType, 10), selected), style)
		} else {
			group := app.groups[row.GroupIndex]
			item := group.Items[row.ItemIndex]
			itemStyle := style
			if rowIndex != app.groupCursor && item.Tag == group.Selected {
				itemStyle = cellStyle(colorGreen, colorBackground)
			}
			delay := "-"
			if item.URLTestTime > 0 {
				delay = formatDelay(item.URLTestDelay)
			}
			marker := " "
			if item.Tag == group.Selected {
				marker = "*"
			}
			drawText(screen, x+2, listY+1+lineIndex, width-4,
				fmt.Sprintf("  %s %-34s  %-10s  %8s", marker, truncate(item.Tag, 34), truncate(item.ItemType, 10), delay), itemStyle)
		}
	}
	if len(rows) == 0 {
		drawText(screen, x+2, listY+1, width-4, "No proxy groups.", cellStyle(colorMuted, colorBackground))
	}
}

func drawConnections(screen tcell.Screen, x, y, width, height int, app *App) {
	detailsHeight := 0
	if app.showDetails {
		detailsHeight = 7
	}
	tableHeight := height - detailsHeight
	if tableHeight < 4 {
		tableHeight = height
		detailsHeight = 0
	}
	title := fmt.Sprintf("Connections: %d active  UP %s  DN %s", app.activeConnectionCount(), formatRate(app.connectionUplink()), formatRate(app.connectionDownlink()))
	drawBox(screen, x, y, width, tableHeight, title)
	if tableHeight >= 3 {
		columns := connectionColumns(width)
		headers := []string{"Destination", "Proto", "Chain", "Rule", "UP/s", "DN/s", "Age", "State"}
		drawColumns(screen, x+1, y+1, columns, headers, cellStyle(colorMuted, colorBackground).Bold(true))
		ids := app.connectionIDs()
		visible := maxInt(tableHeight-3, 1)
		start := 0
		if app.connectionCursor >= visible {
			start = app.connectionCursor - visible + 1
		}
		for line := 0; line < visible && start+line < len(ids); line++ {
			index := start + line
			id := ids[index]
			row := app.connections[id]
			connection := row.Connection
			destination := connection.Domain
			if destination == "" {
				destination = connection.Destination
			}
			chain := connection.Outbound
			if len(connection.ChainList) > 0 {
				chain = strings.Join(connection.ChainList, " -> ")
			}
			age := nowMillis() - connection.CreatedAt
			if row.Closed {
				age = row.ClosedAt - connection.CreatedAt
			}
			values := []string{
				destination,
				connection.Protocol,
				chain,
				connection.Rule,
				formatRate(row.UplinkRate),
				formatRate(row.DownlinkRate),
				formatDuration(age),
				"active",
			}
			style := cellStyle(colorText, colorBackground)
			if row.Closed {
				style = cellStyle(colorMuted, colorBackground)
			}
			if index == app.connectionCursor {
				fillRect(screen, x+1, y+2+line, width-2, 1, selectedStyle())
				style = selectedStyle()
			}
			if row.Closed {
				values[7] = "closed"
			}
			drawColumns(screen, x+1, y+2+line, columns, values, style)
		}
	}
	if detailsHeight > 0 {
		detailY := y + tableHeight
		drawBox(screen, x, detailY, width, detailsHeight, "Connection details")
		_, row, ok := app.selectedConnection()
		if !ok {
			drawText(screen, x+2, detailY+1, width-4, "No connection selected.", cellStyle(colorMuted, colorBackground))
		} else {
			connection := row.Connection
			destination := connection.Destination
			if connection.Domain != "" {
				destination = connection.Domain + " (" + connection.Destination + ")"
			}
			chain := "-"
			if len(connection.ChainList) > 0 {
				chain = strings.Join(connection.ChainList, " -> ")
			}
			lines := []string{
				fmt.Sprintf("%s %s -> %s", connection.Protocol, connection.Source, destination),
				fmt.Sprintf("inbound: %s (%s)", connection.Inbound, connection.InboundType),
				fmt.Sprintf("outbound: %s (%s)", connection.Outbound, connection.OutboundType),
				fmt.Sprintf("network: %s  rule: %s", connection.Network, connection.Rule),
				"chain: " + chain,
				"state: " + map[bool]string{true: "closed", false: "active"}[row.Closed],
			}
			for index, line := range lines {
				drawText(screen, x+2, detailY+1+index, width-4, line, cellStyle(colorText, colorBackground))
			}
		}
	}
}

func drawLogs(screen tcell.Screen, x, y, width, height int, app *App) {
	values := app.filteredLogs()
	title := fmt.Sprintf("Logs: %d shown, filter <= %s", len(values), logLevelName(app.logMinLevel))
	if app.logFollow {
		title += " [follow]"
	}
	drawBox(screen, x, y, width, height, title)
	visible := maxInt(height-2, 1)
	start := app.logStart(len(values), visible)
	for line := 0; line < visible && start+line < len(values); line++ {
		entry := values[start+line]
		name := logLevelName(entry.Level)
		color := colorMuted
		switch entry.Level {
		case 0:
			color = colorPurple
		case 1, 2:
			color = colorRed
		case 3:
			color = colorYellow
		case 4:
			color = colorText
		}
		lineText := fmt.Sprintf("[%-5s] %s", name, strings.ReplaceAll(entry.Message, "\n", " "))
		drawText(screen, x+2, y+1+line, width-4, lineText, cellStyle(color, colorBackground))
	}
	if len(values) == 0 {
		drawText(screen, x+2, y+1, width-4, "No log entries.", cellStyle(colorMuted, colorBackground))
	}
}

func drawSettings(screen tcell.Screen, x, y, width, height int, app *App) {
	connectionHeight := minInt(10, maxInt(height, 1))
	drawBox(screen, x, y, width, connectionHeight, "Connection")
	if connectionHeight >= 7 {
		urlStyle := cellStyle(colorText, colorBackground)
		secretStyle := cellStyle(colorText, colorBackground)
		if app.settingsField == 0 {
			urlStyle = selectedStyle()
		}
		if app.settingsField == 1 {
			secretStyle = selectedStyle()
		}
		drawText(screen, x+2, y+1, width-4, "API URL", cellStyle(colorMuted, colorBackground))
		drawText(screen, x+2, y+2, width-4, fieldDisplay(app.config.URL, false, app.settingsField == 0, app.settingsEditing && app.settingsField == 0, app.settingsCursor), urlStyle)
		drawText(screen, x+2, y+4, width-4, "Secret", cellStyle(colorMuted, colorBackground))
		drawText(screen, x+2, y+5, width-4, fieldDisplay(app.config.Secret, true, app.settingsField == 1, app.settingsEditing && app.settingsField == 1, app.settingsCursor), secretStyle)
		instruction := "Press c to connect with the values above."
		if app.connected {
			instruction = "Connected. Press d to disconnect or edit then c to reconnect."
		}
		drawText(screen, x+2, y+7, width-4, instruction, cellStyle(colorMuted, colorBackground))
	}

	localY := y + connectionHeight
	localHeight := minInt(6, maxInt(y+height-localY, 0))
	if localHeight >= 2 {
		drawBox(screen, x, localY, width, localHeight, "Local settings")
		drawText(screen, x+2, localY+1, width-4, "Terminal mode: dark / 256-color", cellStyle(colorText, colorBackground))
		drawText(screen, x+2, localY+2, width-4, "Config: ~/.config/singbox-go-tui/config", cellStyle(colorText, colorBackground))
	}
	aboutY := localY + localHeight
	if aboutY < y+height {
		drawBox(screen, x, aboutY, width, y+height-aboutY, "About")
		if y+height-aboutY >= 2 {
			drawText(screen, x+2, aboutY+1, width-4, "Direct sing-box 1.14+ gRPC-Web client.", cellStyle(colorText, colorBackground))
			drawText(screen, x+2, aboutY+2, width-4, "No webview, Tauri runtime, or native GUI dependency.", cellStyle(colorMuted, colorBackground))
		}
	}
}

func drawServers(screen tcell.Screen, x, y, width, height int, app *App) {
	if len(app.servers) == 0 {
		drawPlaceholder(screen, x, y, width, height, "No servers configured",
			"Start with --servers PATH to monitor sing-box servers.")
		return
	}
	cols, cardWidth, cardHeight := serverGridColumns(width)
	rowsVisible := maxInt(height/cardHeight, 1)
	app.serversPageSize = rowsVisible * cols
	cursorRow := app.serverCursor / cols
	if cursorRow < app.serverScroll {
		app.serverScroll = cursorRow
	}
	if cursorRow >= app.serverScroll+rowsVisible {
		app.serverScroll = cursorRow - rowsVisible + 1
	}
	for row := 0; row < rowsVisible; row++ {
		for col := 0; col < cols; col++ {
			index := (app.serverScroll+row)*cols + col
			if index >= len(app.servers) {
				return
			}
			drawServerCard(screen, x+col*cardWidth, y+row*cardHeight, cardWidth, cardHeight,
				&app.servers[index], index == app.serverCursor)
		}
	}
}

func serverGridColumns(width int) (int, int, int) {
	const (
		minCardWidth = 30
		cardHeight   = 5
	)
	cols := maxInt(width/minCardWidth, 1)
	return cols, width / cols, cardHeight
}

func drawServerCard(screen tcell.Screen, x, y, width, height int, monitor *ServerMonitor, selected bool) {
	title := monitor.Entry.Name
	titleColor := colorYellow
	switch {
	case monitor.Err != "":
		titleColor = colorRed
	case monitor.Status != nil:
		titleColor = colorGreen
	}
	if selected {
		title = "> " + title
	}
	drawBox(screen, x, y, width, height, "")
	drawText(screen, x+2, y, width-4, " "+truncate(title, width-6)+" ",
		cellStyle(titleColor, colorBackground).Bold(true))
	if height < 3 {
		return
	}
	if monitor.Status != nil {
		if height >= 3 {
			upLine := fmt.Sprintf("UP %-12s %s", formatRate(monitor.Status.Uplink), formatBytes(monitor.Status.UplinkTotal))
			drawText(screen, x+2, y+2, width-4, upLine, cellStyle(colorGreen, colorBackground))
		}
		if height >= 4 {
			downLine := fmt.Sprintf("DN %-12s %s", formatRate(monitor.Status.Downlink), formatBytes(monitor.Status.DownlinkTotal))
			drawText(screen, x+2, y+3, width-4, downLine, cellStyle(colorAccent, colorBackground))
		}
		return
	}
	state := "connecting..."
	stateColor := colorYellow
	if monitor.Err != "" {
		state = truncate("offline: "+monitor.Err, maxInt(width-4, 0))
		stateColor = colorRed
	}
	drawText(screen, x+2, y+2, width-4, state, cellStyle(stateColor, colorBackground))
}

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

func drawHelp(screen tcell.Screen, width, height int) {
	boxWidth := minInt(maxInt(width-4, 30), 72)
	boxHeight := minInt(maxInt(height-4, 12), 16)
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
		"1..5       switch between pages",
		"Tab        next page",
		"r          request all data now",
		"q / Ctrl-C quit",
		"",
		"Groups:    Up/Down, Enter, t, x, [, ], m",
		"Connections: Up/Down, Enter, c, C",
		"Logs:      Up/Down, PgUp/PgDn, Home/End, f, a, c",
		"Settings:  Up/Down, Enter/e, text editing, c, d",
		"Servers:   Up/Down, PgUp/PgDn, Home/End",
		"",
		"Press ? or Esc to close.",
	}
	for index, line := range lines {
		color := colorText
		if strings.HasPrefix(line, "Press ") {
			color = colorYellow
		}
		drawText(screen, boxX+2, boxY+2+index, boxWidth-4, line, cellStyle(color, colorPanel))
	}
}

func renderMetric(screen tcell.Screen, x, y, width, height int, title, value string, color tcell.Color) {
	drawBox(screen, x, y, width, height, title)
	if height >= 3 {
		drawText(screen, x+2, y+2, width-4, value, cellStyle(color, colorBackground).Bold(true))
	}
}

func renderStat(screen tcell.Screen, x, y, width, height int, title, value string) {
	drawBox(screen, x, y, width, height, title)
	if height >= 3 {
		drawText(screen, x+2, y+2, width-4, value, cellStyle(colorText, colorBackground))
	}
}

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
		if x+index < 0 {
			continue
		}
		screen.SetContent(x+index, y, value, nil, style)
	}
}

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

func connectionColumns(width int) []int {
	base := []int{22, 8, 20, 16, 10, 10, 9, 8}
	available := maxInt(width-2, len(base))
	separators := len(base) - 1
	need := 0
	for _, value := range base {
		need += value
	}
	need += separators
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

func fieldDisplay(value string, secret, selected, editing bool, cursor int) string {
	if secret {
		value = strings.Repeat("*", len([]rune(value)))
	}
	if selected && editing {
		runes := []rune(value)
		position := minInt(maxInt(cursor, 0), len(runes))
		runes = append(runes, 0)
		copy(runes[position+1:], runes[position:])
		runes[position] = '|'
		return string(runes)
	}
	return value
}

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

func formatRate(value int64) string {
	return formatBytes(maxInt64(value, 0)) + "/s"
}

func formatDuration(milliseconds int64) string {
	if milliseconds <= 0 {
		return "0s"
	}
	seconds := milliseconds / 1000
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dd %dh", hours/24, hours%24)
}

func formatDelay(delay int32) string {
	if delay <= 0 {
		return "timeout"
	}
	return fmt.Sprintf("%dms", delay)
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
