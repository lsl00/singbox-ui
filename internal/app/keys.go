package app

import "github.com/gdamore/tcell/v2"

// nextView moves to the next page.
func (a *App) nextView() {
	a.View = View((int(a.View) + 1) % ViewCount)
}

// previousView moves to the previous page.
func (a *App) previousView() {
	a.View = View((int(a.View) + ViewCount - 1) % ViewCount)
}

// HandleKey processes one key press.
func (a *App) HandleKey(event *tcell.EventKey) {
	if a.Help {
		if event.Key() == tcell.KeyEscape || event.Rune() == '?' {
			a.Help = false
		}
		return
	}
	if a.SettingsEditing {
		a.handleSettingsEditing(event)
		return
	}
	if event.Key() == tcell.KeyCtrlC {
		a.ShouldQuit = true
		return
	}
	if event.Rune() == '?' {
		a.Help = true
		return
	}
	// The overlay of an opened instance is modal, it takes the keys
	// before the pages do.
	if a.OpenedServer >= 0 {
		a.handleOverlayKey(event)
		return
	}
	if event.Rune() == 'q' {
		a.ShouldQuit = true
		return
	}
	if a.View == ViewSettings && (event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab) {
		a.handleSettingsKey(event)
		return
	}

	switch event.Key() {
	case tcell.KeyTab:
		a.nextView()
	case tcell.KeyBacktab:
		a.previousView()
	case tcell.KeyEscape:
		if a.OpenedServer >= 0 {
			a.CloseServer()
			a.Message = "closed the server"
			return
		}
		a.Message = ""
	default:
		switch event.Rune() {
		case '1':
			a.View = ViewOverview
		case '2':
			a.View = ViewGroups
		case '3':
			a.View = ViewConnections
		case '4':
			a.View = ViewLogs
		case '5':
			a.View = ViewSettings
		case '6':
			a.View = ViewServers
		case 'r':
			a.Refresh()
		default:
			switch a.View {
			case ViewOverview:
				a.handleOverviewKey(event)
			case ViewGroups:
				a.handleGroupsKey(event)
			case ViewConnections:
				a.handleConnectionsKey(event)
			case ViewLogs:
				a.handleLogsKey(event)
			case ViewSettings:
				a.handleSettingsKey(event)
			case ViewServers:
				a.handleServersKey(event)
			}
		}
	}
}

func (a *App) handleOverviewKey(event *tcell.EventKey) {
	switch event.Rune() {
	case 'm':
		a.cycleMode(1)
	case '[':
		a.cycleMode(-1)
	case ']':
		a.cycleMode(1)
	}
}

func (a *App) handleGroupsKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		a.GroupCursor = max(a.GroupCursor-1, 0)
	case tcell.KeyDown:
		count := len(a.Rows())
		if count > 0 {
			a.GroupCursor = min(a.GroupCursor+1, count-1)
		}
	case tcell.KeyEnter:
		a.selectHighlightedNode()
	case tcell.KeyRune:
		switch event.Rune() {
		case 't':
			a.testSelectedNode()
		case 'T':
			a.testSelectedGroup()
		case 'x':
			a.toggleSelectedGroup()
		case 'm', ']':
			a.cycleMode(1)
		case '[':
			a.cycleMode(-1)
		}
	}
}

func (a *App) handleConnectionsKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		a.ConnCursor = max(a.ConnCursor-1, 0)
	case tcell.KeyDown:
		a.ConnCursor = min(a.ConnCursor+1, max(len(a.Connections)-1, 0))
	case tcell.KeyEnter:
		a.ShowDetails = !a.ShowDetails
	case tcell.KeyRune:
		switch event.Rune() {
		case 'c':
			a.closeSelectedConnection()
		case 'C':
			a.closeAllConnections()
		case 's':
			a.ConnSort = ConnSort((int(a.ConnSort) + 1) % 3)
			a.sortConnections()
			a.clampConnCursor()
			a.Message = "connections sorted by " + a.ConnSort.Label()
		}
	}
}

func (a *App) handleLogsKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		a.LogFollow = false
		a.LogOffset++
	case tcell.KeyDown:
		if a.LogOffset <= 1 {
			a.LogOffset = 0
			a.LogFollow = true
		} else {
			a.LogOffset--
		}
	case tcell.KeyPgUp:
		a.LogFollow = false
		a.LogOffset += 10
	case tcell.KeyPgDn:
		a.LogOffset = max(a.LogOffset-10, 0)
		if a.LogOffset == 0 {
			a.LogFollow = true
		}
	case tcell.KeyHome:
		a.LogFollow = false
		a.LogOffset = int(^uint(0) >> 1)
	case tcell.KeyEnd:
		a.LogFollow = true
		a.LogOffset = 0
	case tcell.KeyRune:
		switch event.Rune() {
		case 'f':
			next := int(a.LogLevel.Load()) + 1
			if next > LogLevelError {
				next = LogLevelAll
			}
			a.LogLevel.Store(int32(next))
			a.Message = "log filter: " + logLevelNames[next]
		case 'a':
			a.LogFollow = !a.LogFollow
			if a.LogFollow {
				a.LogOffset = 0
			}
		case 'c':
			a.Logs = []LogLine{}
			a.Message = "local log buffer cleared"
		}
	}
}

// handleOverlayKey processes a key press while the overlay of an opened
// instance is shown.
func (a *App) handleOverlayKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyEscape:
		a.CloseServer()
		a.Message = "closed the server"
		return
	case tcell.KeyUp:
		a.serverConnCursor = max(a.serverConnCursor-1, 0)
		return
	case tcell.KeyDown:
		count := len(a.serverConns.list())
		if count > 0 {
			a.serverConnCursor = min(a.serverConnCursor+1, count-1)
		}
		return
	case tcell.KeyPgUp:
		a.serverLogFoll = false
		a.serverLogOff += 10
		return
	case tcell.KeyPgDn:
		a.serverLogOff = max(a.serverLogOff-10, 0)
		if a.serverLogOff == 0 {
			a.serverLogFoll = true
		}
		return
	case tcell.KeyHome:
		a.serverLogFoll = false
		a.serverLogOff = int(^uint(0) >> 1)
		return
	case tcell.KeyEnd:
		a.serverLogFoll = true
		a.serverLogOff = 0
		return
	}
	if event.Key() != tcell.KeyRune {
		return
	}
	switch event.Rune() {
	case 'q':
		a.ShouldQuit = true
	case 'f':
		next := int(a.LogLevel.Load()) + 1
		if next > LogLevelError {
			next = LogLevelAll
		}
		a.LogLevel.Store(int32(next))
		a.Message = "log filter: " + logLevelNames[next]
	case 'a':
		a.serverLogFoll = !a.serverLogFoll
		if a.serverLogFoll {
			a.serverLogOff = 0
		}
	case 'c':
		a.closeServerConnection()
	case 'C':
		a.closeAllServerConnections()
	}
}

func (a *App) handleServersKey(event *tcell.EventKey) {
	page := max(a.ServersPageSize, 1)
	count := len(a.Servers)
	switch event.Key() {
	case tcell.KeyEnter:
		a.OpenServer(a.ServerCursor)
	case tcell.KeyUp:
		a.ServerCursor = max(a.ServerCursor-1, 0)
	case tcell.KeyDown:
		if count > 0 {
			a.ServerCursor = min(a.ServerCursor+1, count-1)
		}
	case tcell.KeyPgUp:
		a.ServerCursor = max(a.ServerCursor-page, 0)
	case tcell.KeyPgDn:
		if count > 0 {
			a.ServerCursor = min(a.ServerCursor+page, count-1)
		}
	case tcell.KeyHome:
		a.ServerCursor = 0
	case tcell.KeyEnd:
		if count > 0 {
			a.ServerCursor = count - 1
		}
	}
}

// handleSettingsKey processes a key press on the settings page.
func (a *App) handleSettingsKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		a.SettingsField = max(a.SettingsField-1, 0)
	case tcell.KeyDown, tcell.KeyTab:
		a.SettingsField = min(a.SettingsField+1, settingsFieldCount-1)
	case tcell.KeyBacktab:
		a.SettingsField = max(a.SettingsField-1, 0)
	case tcell.KeyEnter:
		a.beginSettingsEdit()
	case tcell.KeyRune:
		switch event.Rune() {
		case 'e':
			a.beginSettingsEdit()
		case 'c':
			// Reconnecting discards the state of the old controller, so
			// an established connection is closed first.
			if a.Connected || a.Connecting {
				a.Disconnect()
			}
			a.Connect()
		case 'd':
			a.Disconnect()
		case 'R':
			a.reloadConfig()
		case 'X':
			a.restartKernel()
		case 'F':
			a.flushDNSCache()
		case 'P':
			a.flushFakeIPCache()
		}
	}
}

// settingsFieldCount is the number of editable settings fields.
const settingsFieldCount = 3

// beginSettingsEdit starts editing the selected field.
func (a *App) beginSettingsEdit() {
	a.SettingsEditing = true
	a.SettingsOriginal = a.settingsValue()
	a.SettingsCursor = len([]rune(a.settingsValue()))
	a.Message = "editing; Enter accepts, Esc cancels editing"
}

// handleSettingsEditing processes a key press while a field is edited.
func (a *App) handleSettingsEditing(event *tcell.EventKey) {
	value := []rune(a.settingsValue())
	switch event.Key() {
	case tcell.KeyEscape:
		a.setSettingsValue(a.SettingsOriginal)
		a.SettingsEditing = false
	case tcell.KeyEnter:
		a.SettingsEditing = false
		a.Message = "field updated; press c to connect"
	case tcell.KeyTab:
		a.SettingsEditing = false
		a.SettingsField = min(a.SettingsField+1, settingsFieldCount-1)
		a.beginSettingsEdit()
	case tcell.KeyLeft:
		a.SettingsCursor = max(a.SettingsCursor-1, 0)
	case tcell.KeyRight:
		a.SettingsCursor = min(a.SettingsCursor+1, len(value))
	case tcell.KeyHome:
		a.SettingsCursor = 0
	case tcell.KeyEnd:
		a.SettingsCursor = len(value)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.SettingsCursor > 0 {
			value = append(value[:a.SettingsCursor-1], value[a.SettingsCursor:]...)
			a.SettingsCursor--
			a.setSettingsValue(string(value))
		}
	case tcell.KeyDelete:
		if a.SettingsCursor < len(value) {
			value = append(value[:a.SettingsCursor], value[a.SettingsCursor+1:]...)
			a.setSettingsValue(string(value))
		}
	case tcell.KeyRune:
		if event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			position := min(a.SettingsCursor, len(value))
			value = append(value, 0)
			copy(value[position+1:], value[position:])
			value[position] = event.Rune()
			a.SettingsCursor = position + 1
			a.setSettingsValue(string(value))
		}
	}
}

func (a *App) settingsValue() string {
	switch a.SettingsField {
	case 0:
		return a.Config.URL
	case 1:
		return a.Config.Secret
	default:
		return a.ConfigPath
	}
}

func (a *App) setSettingsValue(value string) {
	switch a.SettingsField {
	case 0:
		a.Config.URL = value
	case 1:
		a.Config.Secret = value
	default:
		a.ConfigPath = value
	}
}
