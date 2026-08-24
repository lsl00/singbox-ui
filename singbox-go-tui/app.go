package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"
)

const (
	maxHistory = 60
	maxLogs    = 2000
)

type View int

const (
	ViewOverview View = iota
	ViewGroups
	ViewConnections
	ViewLogs
	ViewSettings
	ViewServers
)

func (v View) title() string {
	switch v {
	case ViewOverview:
		return "Overview"
	case ViewGroups:
		return "Proxy Groups"
	case ViewConnections:
		return "Connections"
	case ViewLogs:
		return "Logs"
	case ViewSettings:
		return "Settings"
	case ViewServers:
		return "Servers"
	default:
		return ""
	}
}

func (v View) number() int {
	return int(v) + 1
}

type GroupRow struct {
	GroupIndex int
	ItemIndex  int
	IsItem     bool
}

type ActionKind int

const (
	ActionSelectOutbound ActionKind = iota
	ActionURLTest
	ActionExpandGroup
	ActionSetClashMode
	ActionCloseConnection
	ActionCloseAllConnections
	ActionClearLogs
)

type NetworkKind int

const (
	NetworkConnected NetworkKind = iota
	NetworkStatus
	NetworkServiceStatus
	NetworkStartedAt
	NetworkGroups
	NetworkClashMode
	NetworkConnections
	NetworkLogs
	NetworkAction
	NetworkServerStatus
)

type NetworkEvent struct {
	Kind       NetworkKind
	Generation uint64
	Err        error

	Client        *APIClient
	Version       VersionInfo
	Status        StatusInfo
	ServiceStatus ServiceStatusInfo
	StartedAt     int64
	Groups        []GroupInfo
	ClashMode     ClashModeInfo
	Connections   []ConnectionEventsInfo
	Logs          []LogsInfo

	Action      ActionKind
	ServerIndex int
	Epoch       uint64
}

type ServerMonitor struct {
	Entry     ServerEntry
	Client    *APIClient
	Connected bool
	Status    *StatusInfo
	Err       string
	Busy      bool
}

type App struct {
	config APIConfig
	view   View

	client      *APIClient
	connected   bool
	connecting  bool
	shouldQuit  bool
	help        bool
	version     *VersionInfo
	service     *ServiceStatusInfo
	status      *StatusInfo
	startedAt   int64
	hasStarted  bool
	historyUp   []int64
	historyDown []int64

	groups      []GroupInfo
	groupCursor int
	clashMode   *ClashModeInfo
	expanded    map[string]bool

	connections      map[string]ConnectionRow
	connectionCursor int
	showDetails      bool

	logs        []LogEntryInfo
	logMinLevel int32
	logFollow   bool
	logOffset   int

	settingsField    int
	settingsEditing  bool
	settingsCursor   int
	settingsOriginal string

	servers         []ServerMonitor
	serverCursor    int
	serverScroll    int
	serversPageSize int

	cachedGroupRows []GroupRow

	logsFiltered []LogEntryInfo
	logsStale    bool

	connIDs       []string
	connActive    int
	connUplinkAgg int64
	connDownAgg   int64

	message string
	error   string

	generation      uint64
	groupsEpoch     uint64
	appliedGroups   uint64
	statusBusy      bool
	serviceBusy     bool
	startedBusy     bool
	groupsBusy      bool
	clashBusy       bool
	connectionsBusy bool
	logsBusy        bool
	actionBusy      bool
}

func newApp(config APIConfig) *App {
	return &App{
		config:      config,
		view:        ViewOverview,
		connections: make(map[string]ConnectionRow),
		logMinLevel: 2,
		logFollow:   true,
		generation:  1,
		expanded:    make(map[string]bool),
	}
}

// groupIsExpanded overlays session-local expand toggles onto the server
// state. sing-box only persists expand flags when a cache_file is
// configured; without one SetGroupExpand succeeds but SubscribeGroups
// keeps reporting isExpand=false, so the client must track its own.
func (a *App) groupIsExpanded(group GroupInfo) bool {
	if expanded, ok := a.expanded[group.Tag]; ok {
		return expanded
	}
	return group.IsExpand
}

func (a *App) connect(tx chan<- NetworkEvent) {
	if a.connecting {
		return
	}
	a.generation++
	generation := a.generation
	config := a.config
	a.client = nil
	a.connected = false
	a.connecting = true
	a.resetBusy()
	a.error = ""
	a.message = "connecting to " + config.URL
	go func() {
		client, err := newAPIClient(config)
		if err == nil {
			var version VersionInfo
			version, err = client.getVersion()
			if err == nil {
				tx <- NetworkEvent{
					Kind:       NetworkConnected,
					Generation: generation,
					Client:     client,
					Version:    version,
				}
				return
			}
		}
		tx <- NetworkEvent{Kind: NetworkConnected, Generation: generation, Err: err}
	}()
}

func (a *App) disconnect() {
	a.generation++
	a.client = nil
	a.connected = false
	a.connecting = false
	a.version = nil
	a.service = nil
	a.status = nil
	a.hasStarted = false
	a.historyUp = nil
	a.historyDown = nil
	a.groups = nil
	a.clashMode = nil
	a.connections = make(map[string]ConnectionRow)
	a.logs = nil
	a.expanded = make(map[string]bool)
	a.invalidateGroups()
	a.markLogsStale()
	a.rebuildConnectionIndex()
	a.resetBusy()
	a.message = "disconnected"
	a.error = ""
}

// pollAsync schedules one bounded poll unless an identical request is
// still in flight or the client went away.
func (a *App) pollAsync(tx chan<- NetworkEvent, busy *bool, kind NetworkKind, fetch func(*APIClient) NetworkEvent) {
	if *busy || a.client == nil {
		return
	}
	*busy = true
	client := a.client
	generation := a.generation
	go func() {
		event := fetch(client)
		event.Kind = kind
		event.Generation = generation
		tx <- event
	}()
}

func (a *App) pollStatus(tx chan<- NetworkEvent) {
	a.pollAsync(tx, &a.statusBusy, NetworkStatus, func(client *APIClient) NetworkEvent {
		values, err := client.subscribeStatus()
		event := NetworkEvent{Err: err}
		if err == nil {
			if len(values) == 0 {
				event.Err = fmt.Errorf("no status data received")
			} else {
				event.Status = values[len(values)-1]
			}
		}
		return event
	})
}

func (a *App) pollMetadata(tx chan<- NetworkEvent) {
	a.pollAsync(tx, &a.serviceBusy, NetworkServiceStatus, func(client *APIClient) NetworkEvent {
		values, err := client.subscribeServiceStatus()
		event := NetworkEvent{Err: err}
		if err == nil {
			if len(values) == 0 {
				event.Err = fmt.Errorf("no service status received")
			} else {
				event.ServiceStatus = values[len(values)-1]
			}
		}
		return event
	})
	a.pollAsync(tx, &a.startedBusy, NetworkStartedAt, func(client *APIClient) NetworkEvent {
		startedAt, err := client.getStartedAt()
		return NetworkEvent{StartedAt: startedAt, Err: err}
	})
}

func (a *App) pollGroups(tx chan<- NetworkEvent) {
	epoch := a.groupsEpoch // captured synchronously so later bumps cannot leak in
	a.pollAsync(tx, &a.groupsBusy, NetworkGroups, func(client *APIClient) NetworkEvent {
		groups, err := client.subscribeGroups()
		return NetworkEvent{Groups: groups, Err: err, Epoch: epoch}
	})
	a.pollAsync(tx, &a.clashBusy, NetworkClashMode, func(client *APIClient) NetworkEvent {
		mode, err := client.getClashModeStatus()
		return NetworkEvent{ClashMode: mode, Err: err}
	})
}

func (a *App) pollConnections(tx chan<- NetworkEvent) {
	a.pollAsync(tx, &a.connectionsBusy, NetworkConnections, func(client *APIClient) NetworkEvent {
		values, err := client.subscribeConnections()
		return NetworkEvent{Connections: values, Err: err}
	})
}

func (a *App) pollLogs(tx chan<- NetworkEvent) {
	a.pollAsync(tx, &a.logsBusy, NetworkLogs, func(client *APIClient) NetworkEvent {
		values, err := client.subscribeLogs()
		return NetworkEvent{Logs: values, Err: err}
	})
}

func (a *App) pollAll(tx chan<- NetworkEvent) {
	a.pollStatus(tx)
	a.pollMetadata(tx)
	a.pollGroups(tx)
	a.pollConnections(tx)
	a.pollLogs(tx)
	a.pollServers(tx)
	a.message = "refresh requested"
}

func (a *App) loadServers(path string) {
	entries := loadServerEntries(path)
	monitors := make([]ServerMonitor, 0, len(entries))
	for _, entry := range entries {
		monitor := ServerMonitor{Entry: entry}
		client, err := newAPIClient(APIConfig{URL: entry.BaseURL})
		if err != nil {
			monitor.Err = err.Error()
		} else {
			monitor.Client = client
		}
		monitors = append(monitors, monitor)
	}
	a.servers = monitors
	a.serverCursor = 0
	a.serverScroll = 0
}

func (a *App) pollServers(tx chan<- NetworkEvent) {
	for index := range a.servers {
		monitor := &a.servers[index]
		if monitor.Busy || monitor.Client == nil {
			continue
		}
		monitor.Busy = true
		client := monitor.Client
		go func(index int) {
			values, err := client.subscribeStatus()
			event := NetworkEvent{Kind: NetworkServerStatus, ServerIndex: index, Err: err}
			if err == nil {
				if len(values) == 0 {
					event.Err = fmt.Errorf("no status data received")
				} else {
					event.Status = values[len(values)-1]
				}
			}
			tx <- event
		}(index)
	}
}

func (a *App) resetBusy() {
	a.statusBusy = false
	a.serviceBusy = false
	a.startedBusy = false
	a.groupsBusy = false
	a.clashBusy = false
	a.connectionsBusy = false
	a.logsBusy = false
	a.actionBusy = false
}

func (a *App) handleNetwork(event NetworkEvent, tx chan<- NetworkEvent) {
	// Server monitors are independent of the main client connection, so
	// their events bypass the generation gate below.
	if event.Kind == NetworkServerStatus {
		a.handleServerStatus(event)
		return
	}
	if event.Generation != a.generation {
		return
	}
	switch event.Kind {
	case NetworkConnected:
		a.connecting = false
		if event.Err != nil {
			a.client = nil
			a.connected = false
			a.error = event.Err.Error()
			a.message = "connection failed; press c in Settings to retry"
			return
		}
		a.client = event.Client
		a.connected = true
		version := event.Version
		a.version = &version
		a.service = nil
		a.status = nil
		a.hasStarted = false
		a.historyUp = nil
		a.historyDown = nil
		a.groups = nil
		a.clashMode = nil
		a.connections = make(map[string]ConnectionRow)
		a.logs = nil
		a.expanded = make(map[string]bool)
		a.invalidateGroups()
		a.markLogsStale()
		a.rebuildConnectionIndex()
		a.error = ""
		a.message = fmt.Sprintf("connected to sing-box %s (API v%d)", version.Version, version.APIVersion)
		if err := saveConfig(a.config); err != nil {
			a.error = "connected, but could not save config: " + err.Error()
		}
		a.pollAll(tx)

	case NetworkStatus:
		a.statusBusy = false
		if event.Err != nil {
			a.connected = false
			a.error = event.Err.Error()
			return
		}
		a.connected = true
		status := event.Status
		a.status = &status
		a.historyUp = appendBounded(a.historyUp, maxInt64(status.Uplink, 0), maxHistory)
		a.historyDown = appendBounded(a.historyDown, maxInt64(status.Downlink, 0), maxHistory)
		a.error = ""

	case NetworkServiceStatus:
		a.serviceBusy = false
		if event.Err == nil {
			status := event.ServiceStatus
			a.service = &status
		}

	case NetworkStartedAt:
		a.startedBusy = false
		if event.Err == nil {
			a.startedAt = event.StartedAt
			a.hasStarted = true
		}

	case NetworkGroups:
		a.groupsBusy = false
		if event.Err != nil {
			a.error = event.Err.Error()
		} else if event.Epoch >= a.appliedGroups {
			// Snapshots initiated before a recent mutation (expand, select,
			// URL test, mode switch) carry an older epoch and must not
			// overwrite the newer state that the mutation produced.
			a.appliedGroups = event.Epoch
			a.groups = event.Groups
			a.invalidateGroups()
			a.clampGroupCursor()
		}

	case NetworkClashMode:
		a.clashBusy = false
		if event.Err == nil {
			mode := event.ClashMode
			a.clashMode = &mode
		}

	case NetworkConnections:
		a.connectionsBusy = false
		if event.Err != nil {
			a.error = event.Err.Error()
		} else {
			a.applyConnectionBatches(event.Connections)
			a.clampConnectionCursor()
		}

	case NetworkLogs:
		a.logsBusy = false
		if event.Err != nil {
			a.error = event.Err.Error()
		} else {
			a.applyLogBatches(event.Logs)
		}

	case NetworkAction:
		a.actionBusy = false
		if event.Err != nil {
			a.error = event.Err.Error()
			return
		}
		a.error = ""
		switch event.Action {
		case ActionSelectOutbound:
			a.finishGroupAction("outbound selected", tx)
		case ActionURLTest:
			a.finishGroupAction("URL test completed", tx)
		case ActionExpandGroup:
			a.finishGroupAction("group display updated", tx)
		case ActionSetClashMode:
			a.finishGroupAction("Clash mode updated", tx)
		case ActionCloseConnection:
			a.message = "connection close requested"
			a.pollConnections(tx)
		case ActionCloseAllConnections:
			a.message = "all connection close requested"
			a.pollConnections(tx)
		case ActionClearLogs:
			a.message = "logs cleared"
			a.logs = nil
			a.markLogsStale()
		}
	}
}

// finishGroupAction records the outcome of an action that changes group
// data, bumps the epoch so in-flight pre-action snapshots are dropped,
// and schedules a fresh groups fetch.
func (a *App) finishGroupAction(message string, tx chan<- NetworkEvent) {
	a.message = message
	a.groupsEpoch++
	a.pollGroups(tx)
}

func appendBounded(values []int64, value int64, limit int) []int64 {
	values = append(values, value)
	if len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}

func (a *App) handleServerStatus(event NetworkEvent) {
	if event.ServerIndex < 0 || event.ServerIndex >= len(a.servers) {
		return
	}
	monitor := &a.servers[event.ServerIndex]
	monitor.Busy = false
	if event.Err != nil {
		monitor.Connected = false
		monitor.Err = event.Err.Error()
		return
	}
	status := event.Status
	monitor.Connected = true
	monitor.Status = &status
	monitor.Err = ""
}

func (a *App) handleKey(event *tcell.EventKey, tx chan<- NetworkEvent) {
	if a.help {
		if event.Key() == tcell.KeyEscape || event.Rune() == '?' {
			a.help = false
		}
		return
	}
	if a.settingsEditing {
		a.handleSettingsEditing(event)
		return
	}
	if event.Key() == tcell.KeyCtrlC || event.Rune() == 'q' {
		a.shouldQuit = true
		return
	}
	if event.Rune() == '?' {
		a.help = true
		return
	}
	if a.view == ViewSettings && (event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab) {
		a.handleSettingsKey(event, tx)
		return
	}

	switch event.Key() {
	case tcell.KeyTab:
		a.nextView()
	case tcell.KeyBacktab:
		a.previousView()
	case tcell.KeyEscape:
		a.message = ""
	default:
		switch event.Rune() {
		case '1':
			a.view = ViewOverview
		case '2':
			a.view = ViewGroups
		case '3':
			a.view = ViewConnections
		case '4':
			a.view = ViewLogs
		case '5':
			a.view = ViewSettings
		case '6':
			a.view = ViewServers
		case 'r':
			a.pollAll(tx)
		default:
			switch a.view {
			case ViewGroups:
				a.handleGroupsKey(event, tx)
			case ViewConnections:
				a.handleConnectionsKey(event, tx)
			case ViewLogs:
				a.handleLogsKey(event, tx)
			case ViewSettings:
				a.handleSettingsKey(event, tx)
			case ViewServers:
				a.handleServersKey(event)
			}
		}
	}
}

func (a *App) handleGroupsKey(event *tcell.EventKey, tx chan<- NetworkEvent) {
	switch event.Key() {
	case tcell.KeyUp:
		a.groupCursor = maxInt(a.groupCursor-1, 0)
	case tcell.KeyDown:
		count := len(a.groupRows())
		if count > 0 {
			a.groupCursor = minInt(a.groupCursor+1, count-1)
		}
	case tcell.KeyEnter:
		row, ok := a.selectedGroupRow()
		if !ok || !row.IsItem || row.GroupIndex >= len(a.groups) {
			return
		}
		group := a.groups[row.GroupIndex]
		if group.Selectable && row.ItemIndex < len(group.Items) {
			item := group.Items[row.ItemIndex]
			a.startAction(tx, ActionSelectOutbound, "selecting "+item.Tag, func(client *APIClient) error {
				return client.selectOutbound(group.Tag, item.Tag)
			})
		}
	case tcell.KeyRune:
		switch event.Rune() {
		case 't':
			row, ok := a.selectedGroupRow()
			if ok && row.IsItem && row.GroupIndex < len(a.groups) {
				group := a.groups[row.GroupIndex]
				if row.ItemIndex < len(group.Items) {
					item := group.Items[row.ItemIndex]
					a.startAction(tx, ActionURLTest, "testing "+item.Tag, func(client *APIClient) error {
						err := client.urlTest(item.Tag)
						if err == nil {
							time.Sleep(1500 * time.Millisecond)
						}
						return err
					})
				}
			}
		case 'x':
			row, ok := a.selectedGroupRow()
			if !ok || row.GroupIndex >= len(a.groups) {
				return
			}
			group := a.groups[row.GroupIndex]
			nextExpand := !a.groupIsExpanded(group)
			a.expanded[group.Tag] = nextExpand
			a.invalidateGroups()
			a.clampGroupCursor()
			groupTag := group.Tag
			a.startAction(tx, ActionExpandGroup, "", func(client *APIClient) error {
				return client.setGroupExpand(groupTag, nextExpand)
			})
		case 'm', ']':
			a.cycleClashMode(tx, 1)
		case '[':
			a.cycleClashMode(tx, -1)
		}
	}
}

func (a *App) handleConnectionsKey(event *tcell.EventKey, tx chan<- NetworkEvent) {
	switch event.Key() {
	case tcell.KeyUp:
		a.connectionCursor = maxInt(a.connectionCursor-1, 0)
	case tcell.KeyDown:
		a.connectionCursor = minInt(a.connectionCursor+1, maxInt(len(a.connections)-1, 0))
	case tcell.KeyEnter:
		a.showDetails = !a.showDetails
	case tcell.KeyRune:
		switch event.Rune() {
		case 'c':
			id, row, ok := a.selectedConnection()
			if ok && !row.Closed {
				a.startAction(tx, ActionCloseConnection, "", func(client *APIClient) error {
					return client.closeConnection(id)
				})
			}
		case 'C':
			a.startAction(tx, ActionCloseAllConnections, "closing all active connections", func(client *APIClient) error {
				return client.closeAllConnections()
			})
		}
	}
}

func (a *App) handleLogsKey(event *tcell.EventKey, tx chan<- NetworkEvent) {
	switch event.Key() {
	case tcell.KeyUp:
		a.logFollow = false
		a.logOffset++
	case tcell.KeyDown:
		if a.logOffset <= 1 {
			a.logOffset = 0
			a.logFollow = true
		} else {
			a.logOffset--
		}
	case tcell.KeyPgUp:
		a.logFollow = false
		a.logOffset += 10
	case tcell.KeyPgDn:
		a.logOffset = maxInt(a.logOffset-10, 0)
		if a.logOffset == 0 {
			a.logFollow = true
		}
	case tcell.KeyHome:
		a.logFollow = false
		a.logOffset = int(^uint(0) >> 1)
	case tcell.KeyEnd:
		a.logFollow = true
		a.logOffset = 0
	case tcell.KeyRune:
		switch event.Rune() {
		case 'f':
			a.logMinLevel++
			if a.logMinLevel > 6 {
				a.logMinLevel = 0
			}
			a.markLogsStale()
		case 'a':
			a.logFollow = !a.logFollow
			if a.logFollow {
				a.logOffset = 0
			}
		case 'c':
			a.startAction(tx, ActionClearLogs, "clearing logs", func(client *APIClient) error {
				return client.clearLogs()
			})
		}
	}
}

func (a *App) handleServersKey(event *tcell.EventKey) {
	page := maxInt(a.serversPageSize, 1)
	count := len(a.servers)
	switch event.Key() {
	case tcell.KeyUp:
		a.serverCursor = maxInt(a.serverCursor-1, 0)
	case tcell.KeyDown:
		if count > 0 {
			a.serverCursor = minInt(a.serverCursor+1, count-1)
		}
	case tcell.KeyPgUp:
		a.serverCursor = maxInt(a.serverCursor-page, 0)
	case tcell.KeyPgDn:
		if count > 0 {
			a.serverCursor = minInt(a.serverCursor+page, count-1)
		}
	case tcell.KeyHome:
		a.serverCursor = 0
	case tcell.KeyEnd:
		if count > 0 {
			a.serverCursor = count - 1
		}
	}
}

func (a *App) handleSettingsKey(event *tcell.EventKey, tx chan<- NetworkEvent) {
	switch event.Key() {
	case tcell.KeyUp:
		a.settingsField = maxInt(a.settingsField-1, 0)
	case tcell.KeyDown, tcell.KeyTab:
		a.settingsField = minInt(a.settingsField+1, 1)
	case tcell.KeyBacktab:
		a.settingsField = maxInt(a.settingsField-1, 0)
	case tcell.KeyEnter:
		a.beginSettingsEdit()
	case tcell.KeyRune:
		switch event.Rune() {
		case 'e':
			a.beginSettingsEdit()
		case 'c':
			a.connect(tx)
		case 'd':
			a.disconnect()
		}
	}
}

func (a *App) beginSettingsEdit() {
	a.settingsEditing = true
	a.settingsOriginal = a.settingsValue()
	a.settingsCursor = len([]rune(a.settingsValue()))
	a.message = "editing; Enter accepts, Esc cancels editing"
}

func (a *App) handleSettingsEditing(event *tcell.EventKey) {
	value := []rune(a.settingsValue())
	switch event.Key() {
	case tcell.KeyEscape:
		a.setSettingsValue(a.settingsOriginal)
		a.settingsEditing = false
	case tcell.KeyEnter:
		a.settingsEditing = false
		a.message = "field updated; press c to connect"
	case tcell.KeyTab:
		a.settingsEditing = false
		a.settingsField = minInt(a.settingsField+1, 1)
		a.beginSettingsEdit()
	case tcell.KeyLeft:
		a.settingsCursor = maxInt(a.settingsCursor-1, 0)
	case tcell.KeyRight:
		a.settingsCursor = minInt(a.settingsCursor+1, len(value))
	case tcell.KeyHome:
		a.settingsCursor = 0
	case tcell.KeyEnd:
		a.settingsCursor = len(value)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.settingsCursor > 0 {
			value = append(value[:a.settingsCursor-1], value[a.settingsCursor:]...)
			a.settingsCursor--
			a.setSettingsValue(string(value))
		}
	case tcell.KeyDelete:
		if a.settingsCursor < len(value) {
			value = append(value[:a.settingsCursor], value[a.settingsCursor+1:]...)
			a.setSettingsValue(string(value))
		}
	case tcell.KeyRune:
		if event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			position := minInt(a.settingsCursor, len(value))
			value = append(value, 0)
			copy(value[position+1:], value[position:])
			value[position] = event.Rune()
			a.settingsCursor = position + 1
			a.setSettingsValue(string(value))
		}
	}
}

func (a *App) settingsValue() string {
	if a.settingsField == 0 {
		return a.config.URL
	}
	return a.config.Secret
}

func (a *App) setSettingsValue(value string) {
	if a.settingsField == 0 {
		a.config.URL = value
	} else {
		a.config.Secret = value
	}
}

func (a *App) startAction(tx chan<- NetworkEvent, kind ActionKind, message string, action func(*APIClient) error) {
	if a.client == nil {
		return
	}
	if a.actionBusy {
		a.message = "previous action still in progress"
		return
	}
	client := a.client
	generation := a.generation
	a.actionBusy = true
	if message != "" {
		a.message = message
	}
	go func() {
		tx <- NetworkEvent{
			Kind:       NetworkAction,
			Generation: generation,
			Action:     kind,
			Err:        action(client),
		}
	}()
}

func (a *App) cycleClashMode(tx chan<- NetworkEvent, direction int) {
	if a.clashMode == nil || len(a.clashMode.ModeList) == 0 {
		return
	}
	current := 0
	for index, mode := range a.clashMode.ModeList {
		if mode == a.clashMode.CurrentMode {
			current = index
			break
		}
	}
	count := len(a.clashMode.ModeList)
	next := (current + direction) % count
	if next < 0 {
		next += count
	}
	mode := a.clashMode.ModeList[next]
	a.startAction(tx, ActionSetClashMode, "setting Clash mode to "+mode, func(client *APIClient) error {
		return client.setClashMode(mode)
	})
}

// groupRows caches the flattened group/item row list; it only changes
// when the groups snapshot or a local expand toggle lands, both of
// which call invalidateGroups.
func (a *App) groupRows() []GroupRow {
	if a.cachedGroupRows == nil {
		rows := make([]GroupRow, 0)
		for groupIndex, group := range a.groups {
			rows = append(rows, GroupRow{GroupIndex: groupIndex})
			if a.groupIsExpanded(group) {
				for itemIndex := range group.Items {
					rows = append(rows, GroupRow{GroupIndex: groupIndex, ItemIndex: itemIndex, IsItem: true})
				}
			}
		}
		a.cachedGroupRows = rows
	}
	return a.cachedGroupRows
}

func (a *App) invalidateGroups() {
	a.cachedGroupRows = nil
}

func (a *App) selectedGroupRow() (GroupRow, bool) {
	rows := a.groupRows()
	if a.groupCursor < 0 || a.groupCursor >= len(rows) {
		return GroupRow{}, false
	}
	return rows[a.groupCursor], true
}

// rebuildConnectionIndex recomputes the sorted ID list and the
// aggregates shown in the connections header in a single pass. It runs
// when a connection batch lands instead of once per drawn frame.
func (a *App) rebuildConnectionIndex() {
	ids := make([]string, 0, len(a.connections))
	active := 0
	var uplink, downlink int64
	for id, row := range a.connections {
		ids = append(ids, id)
		if !row.Closed {
			active++
			uplink += maxInt64(row.UplinkRate, 0)
			downlink += maxInt64(row.DownlinkRate, 0)
		}
	}
	sort.Strings(ids)
	a.connIDs = ids
	a.connActive = active
	a.connUplinkAgg = uplink
	a.connDownAgg = downlink
}

func (a *App) connectionIDs() []string {
	return a.connIDs
}

func (a *App) selectedConnection() (string, ConnectionRow, bool) {
	if a.connectionCursor < 0 || a.connectionCursor >= len(a.connIDs) {
		return "", ConnectionRow{}, false
	}
	id := a.connIDs[a.connectionCursor]
	row, ok := a.connections[id]
	return id, row, ok
}

func (a *App) activeConnectionCount() int {
	return a.connActive
}

func (a *App) connectionUplink() int64 {
	return a.connUplinkAgg
}

func (a *App) connectionDownlink() int64 {
	return a.connDownAgg
}

// filteredLogs caches the level-filtered view and only rebuilds after
// new logs arrive or the filter level changes (logsStale).
func (a *App) filteredLogs() []LogEntryInfo {
	if a.logsStale {
		values := make([]LogEntryInfo, 0, len(a.logs))
		for _, entry := range a.logs {
			if entry.Level <= a.logMinLevel {
				values = append(values, entry)
			}
		}
		a.logsFiltered = values
		a.logsStale = false
	}
	return a.logsFiltered
}

func (a *App) markLogsStale() {
	a.logsStale = true
}

func (a *App) logStart(total, height int) int {
	if total <= height {
		return 0
	}
	if a.logFollow {
		return total - height
	}
	if a.logOffset == int(^uint(0)>>1) {
		return 0
	}
	return maxInt(total-a.logOffset-height, 0)
}

func (a *App) applyConnectionBatches(batches []ConnectionEventsInfo) {
	for _, batch := range batches {
		if batch.Reset {
			a.connections = make(map[string]ConnectionRow)
		}
		for _, event := range batch.Events {
			a.applyConnectionEvent(event)
		}
	}
	a.rebuildConnectionIndex()
}

func (a *App) applyConnectionEvent(event ConnectionEventInfo) {
	switch event.EventType {
	case 0:
		if event.Connection != nil {
			a.connections[event.ID] = ConnectionRow{Connection: *event.Connection}
		}
	case 1:
		row, ok := a.connections[event.ID]
		if !ok {
			if event.Connection == nil {
				return
			}
			row = ConnectionRow{Connection: *event.Connection}
		}
		row.UplinkRate = event.UplinkDelta
		row.DownlinkRate = event.DownlinkDelta
		if event.Connection != nil {
			row.Connection = *event.Connection
		}
		a.connections[event.ID] = row
	case 2:
		closedAt := event.ClosedAt
		if closedAt <= 0 {
			closedAt = nowMillis()
		}
		row, ok := a.connections[event.ID]
		if !ok && event.Connection == nil {
			return
		}
		if event.Connection != nil {
			row.Connection = *event.Connection
		}
		row.UplinkRate = 0
		row.DownlinkRate = 0
		row.ClosedAt = closedAt
		row.Closed = true
		a.connections[event.ID] = row
	}
}

func (a *App) applyLogBatches(batches []LogsInfo) {
	for _, batch := range batches {
		if batch.Reset {
			a.logs = nil
		}
		a.logs = append(a.logs, batch.Messages...)
	}
	if len(a.logs) > maxLogs {
		a.logs = a.logs[len(a.logs)-maxLogs:]
	}
	a.markLogsStale()
}

func (a *App) clampGroupCursor() {
	count := len(a.groupRows())
	if count == 0 {
		a.groupCursor = 0
	} else {
		a.groupCursor = minInt(a.groupCursor, count-1)
	}
}

func (a *App) clampConnectionCursor() {
	if len(a.connections) == 0 {
		a.connectionCursor = 0
	} else {
		a.connectionCursor = minInt(a.connectionCursor, len(a.connections)-1)
	}
}

func (a *App) nextView() {
	a.view = View((int(a.view) + 1) % 6)
}

func (a *App) previousView() {
	a.view = View((int(a.view) + 5) % 6)
}

func serviceStatusLabel(status ServiceStatusInfo) string {
	switch status.Status {
	case 0:
		return "idle"
	case 1:
		return "starting"
	case 2:
		return "started"
	case 3:
		return "stopping"
	case 4:
		return "fatal"
	default:
		return "unknown"
	}
}

func logLevelName(level int32) string {
	switch level {
	case 0:
		return "PANIC"
	case 1:
		return "FATAL"
	case 2:
		return "ERROR"
	case 3:
		return "WARN"
	case 4:
		return "INFO"
	case 5:
		return "DEBUG"
	case 6:
		return "TRACE"
	default:
		return "?"
	}
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
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
