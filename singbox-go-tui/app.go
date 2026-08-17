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

	Action ActionKind
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

	connections      map[string]ConnectionRow
	connectionCursor int
	showDetails      bool

	logs        []LogRow
	logMinLevel int32
	logFollow   bool
	logOffset   int

	settingsField    int
	settingsEditing  bool
	settingsCursor   int
	settingsOriginal string

	message string
	error   string

	generation      uint64
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
	}
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
	a.resetBusy()
	a.message = "disconnected"
	a.error = ""
}

func (a *App) pollStatus(tx chan<- NetworkEvent) {
	if a.statusBusy || a.client == nil {
		return
	}
	client := a.client
	generation := a.generation
	a.statusBusy = true
	go func() {
		values, err := client.subscribeStatus()
		event := NetworkEvent{Kind: NetworkStatus, Generation: generation, Err: err}
		if err == nil {
			if len(values) == 0 {
				event.Err = fmt.Errorf("no status data received")
			} else {
				event.Status = values[len(values)-1]
			}
		}
		tx <- event
	}()
}

func (a *App) pollMetadata(tx chan<- NetworkEvent) {
	if !a.serviceBusy && a.client != nil {
		client := a.client
		generation := a.generation
		a.serviceBusy = true
		go func() {
			values, err := client.subscribeServiceStatus()
			event := NetworkEvent{Kind: NetworkServiceStatus, Generation: generation, Err: err}
			if err == nil {
				if len(values) == 0 {
					event.Err = fmt.Errorf("no service status received")
				} else {
					event.ServiceStatus = values[len(values)-1]
				}
			}
			tx <- event
		}()
	}
	if !a.startedBusy && a.client != nil {
		client := a.client
		generation := a.generation
		a.startedBusy = true
		go func() {
			startedAt, err := client.getStartedAt()
			tx <- NetworkEvent{
				Kind:       NetworkStartedAt,
				Generation: generation,
				StartedAt:  startedAt,
				Err:        err,
			}
		}()
	}
}

func (a *App) pollGroups(tx chan<- NetworkEvent) {
	if !a.groupsBusy && a.client != nil {
		client := a.client
		generation := a.generation
		a.groupsBusy = true
		go func() {
			groups, err := client.subscribeGroups()
			tx <- NetworkEvent{Kind: NetworkGroups, Generation: generation, Groups: groups, Err: err}
		}()
	}
	if !a.clashBusy && a.client != nil {
		client := a.client
		generation := a.generation
		a.clashBusy = true
		go func() {
			mode, err := client.getClashModeStatus()
			tx <- NetworkEvent{Kind: NetworkClashMode, Generation: generation, ClashMode: mode, Err: err}
		}()
	}
}

func (a *App) pollConnections(tx chan<- NetworkEvent) {
	if a.connectionsBusy || a.client == nil {
		return
	}
	client := a.client
	generation := a.generation
	a.connectionsBusy = true
	go func() {
		values, err := client.subscribeConnections()
		tx <- NetworkEvent{Kind: NetworkConnections, Generation: generation, Connections: values, Err: err}
	}()
}

func (a *App) pollLogs(tx chan<- NetworkEvent) {
	if a.logsBusy || a.client == nil {
		return
	}
	client := a.client
	generation := a.generation
	a.logsBusy = true
	go func() {
		values, err := client.subscribeLogs()
		tx <- NetworkEvent{Kind: NetworkLogs, Generation: generation, Logs: values, Err: err}
	}()
}

func (a *App) pollAll(tx chan<- NetworkEvent) {
	a.pollStatus(tx)
	a.pollMetadata(tx)
	a.pollGroups(tx)
	a.pollConnections(tx)
	a.pollLogs(tx)
	a.message = "refresh requested"
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
		} else {
			a.groups = event.Groups
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
			a.message = "outbound selected"
			a.pollGroups(tx)
		case ActionURLTest:
			a.message = "URL test completed"
			a.pollGroups(tx)
		case ActionExpandGroup:
			a.message = "group display updated"
			a.pollGroups(tx)
		case ActionSetClashMode:
			a.message = "Clash mode updated"
			a.pollGroups(tx)
		case ActionCloseConnection:
			a.message = "connection close requested"
			a.pollConnections(tx)
		case ActionCloseAllConnections:
			a.message = "all connection close requested"
			a.pollConnections(tx)
		case ActionClearLogs:
			a.message = "logs cleared"
			a.logs = nil
		}
	}
}

func appendBounded(values []int64, value int64, limit int) []int64 {
	values = append(values, value)
	if len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
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
			if ok && row.GroupIndex < len(a.groups) {
				group := a.groups[row.GroupIndex]
				a.startAction(tx, ActionExpandGroup, "", func(client *APIClient) error {
					return client.setGroupExpand(group.Tag, !group.IsExpand)
				})
			}
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
	if a.actionBusy || a.client == nil {
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

func (a *App) groupRows() []GroupRow {
	rows := make([]GroupRow, 0)
	for groupIndex, group := range a.groups {
		rows = append(rows, GroupRow{GroupIndex: groupIndex})
		if group.IsExpand {
			for itemIndex := range group.Items {
				rows = append(rows, GroupRow{GroupIndex: groupIndex, ItemIndex: itemIndex, IsItem: true})
			}
		}
	}
	return rows
}

func (a *App) selectedGroupRow() (GroupRow, bool) {
	rows := a.groupRows()
	if a.groupCursor < 0 || a.groupCursor >= len(rows) {
		return GroupRow{}, false
	}
	return rows[a.groupCursor], true
}

func (a *App) connectionIDs() []string {
	ids := make([]string, 0, len(a.connections))
	for id := range a.connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *App) selectedConnection() (string, ConnectionRow, bool) {
	ids := a.connectionIDs()
	if a.connectionCursor < 0 || a.connectionCursor >= len(ids) {
		return "", ConnectionRow{}, false
	}
	id := ids[a.connectionCursor]
	row, ok := a.connections[id]
	return id, row, ok
}

func (a *App) activeConnectionCount() int {
	count := 0
	for _, row := range a.connections {
		if !row.Closed {
			count++
		}
	}
	return count
}

func (a *App) connectionUplink() int64 {
	var value int64
	for _, row := range a.connections {
		if !row.Closed {
			value += maxInt64(row.UplinkRate, 0)
		}
	}
	return value
}

func (a *App) connectionDownlink() int64 {
	var value int64
	for _, row := range a.connections {
		if !row.Closed {
			value += maxInt64(row.DownlinkRate, 0)
		}
	}
	return value
}

func (a *App) filteredLogs() []LogRow {
	values := make([]LogRow, 0, len(a.logs))
	for _, entry := range a.logs {
		if entry.Level <= a.logMinLevel {
			values = append(values, entry)
		}
	}
	return values
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
		for _, entry := range batch.Messages {
			a.logs = append(a.logs, LogRow{Level: entry.Level, Message: entry.Message})
		}
	}
	if len(a.logs) > maxLogs {
		a.logs = a.logs[len(a.logs)-maxLogs:]
	}
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
	a.view = View((int(a.view) + 1) % 5)
}

func (a *App) previousView() {
	a.view = View((int(a.view) + 4) % 5)
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
