package app

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
)

const (
	// maxHistory is the number of rate samples kept for the sparkline.
	maxHistory = 60
	// maxLogs is the number of log lines kept in memory.
	maxLogs = 2000
	// maxConns bounds the number of tracked connections kept in memory.
	maxConns = 5000
)

// Log levels, ordered by severity. The zero value keeps every line.
const (
	LogLevelAll = iota
	LogLevelInfo
	LogLevelWarning
	LogLevelError
)

var logLevelNames = []string{"all", "info", "warning", "error"}
var logLevelOrder = map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3}

// ConnSort selects the ordering of the connections table.
type ConnSort int

// The available connection orderings.
const (
	ConnSortRecent ConnSort = iota
	ConnSortDownload
	ConnSortUpload
)

func (s ConnSort) Label() string {
	switch s {
	case ConnSortDownload:
		return "download"
	case ConnSortUpload:
		return "upload"
	default:
		return "recent"
	}
}

// GroupRow is one line of the flattened proxy group tree.
type GroupRow struct {
	GroupIndex int
	ItemIndex  int
	IsItem     bool
}

// LogLine is one buffered log record.
type LogLine struct {
	Time  time.Time
	Level string
	Text  string
}

// App holds the whole dashboard state. Only the main goroutine touches it.
type App struct {
	Config  config.APIConfig
	View    View
	Help    bool
	Servers []ServerMonitor

	Connected  bool
	Connecting bool
	ShouldQuit bool

	Version *clash.Version
	Runtime *clash.Runtime

	Traffic   clash.Traffic
	TrafficUp bool
	Memory    uint64
	MemoryUp  bool

	HistoryUp   []int64
	HistoryDown []int64

	Proxies      map[string]clash.Proxy
	GroupNames   []string
	Expanded     map[string]bool
	GroupCursor  int
	GroupRows    []GroupRow
	GroupRowsOK  bool
	SelectedNode string

	Connections []clash.Connection
	ConnCursor  int
	ConnSort    ConnSort
	ShowDetails bool

	Logs      []LogLine
	LogLevel  atomic.Int32
	LogFollow bool
	LogOffset int

	SettingsField    int
	SettingsEditing  bool
	SettingsCursor   int
	SettingsOriginal string
	ConfigPath       string

	ServerCursor    int
	ServerScroll    int
	ServersPageSize int

	Message string
	Error   string

	StreamUp map[string]bool

	client *clash.Client
	gen    uint64
	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	busy   map[EventKind]bool
}

// New creates a dashboard for the given controller configuration.
func New(api config.APIConfig) *App {
	app := &App{
		Config:      api,
		View:        ViewOverview,
		Proxies:     map[string]clash.Proxy{},
		Expanded:    map[string]bool{},
		Connections: []clash.Connection{},
		Logs:        []LogLine{},
		LogFollow:   true,
		StreamUp:    map[string]bool{},
		events:      make(chan Event, 256),
		busy:        map[EventKind]bool{},
	}
	app.LogLevel.Store(LogLevelAll)
	return app
}

// Events returns the channel carrying network results.
func (a *App) Events() <-chan Event {
	return a.events
}

// Connect opens a connection to the controller and starts the streams.
func (a *App) Connect() {
	if a.Connecting {
		return
	}
	a.gen++
	gen := a.gen
	a.stopStreams()
	a.client = nil
	a.Connected = false
	a.Connecting = true
	a.resetBusy()
	a.Error = ""
	a.Message = "connecting to " + a.Config.URL

	go func() {
		client, err := clash.New(clash.Config{URL: a.Config.URL, Secret: a.Config.Secret})
		var version *clash.Version
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			obtained, versionErr := client.Version(ctx)
			cancel()
			if versionErr != nil {
				err = versionErr
			} else {
				version = &obtained
			}
		}
		a.events <- Event{Kind: EventVersion, Gen: gen, Err: err, Client: client, Version: version}
	}()
}

// Disconnect closes the connection and stops every stream.
func (a *App) Disconnect() {
	a.gen++
	a.stopStreams()
	a.client = nil
	a.Connected = false
	a.Connecting = false
	a.Version = nil
	a.Runtime = nil
	a.Traffic = clash.Traffic{}
	a.TrafficUp = false
	a.Memory = 0
	a.MemoryUp = false
	a.HistoryUp = nil
	a.HistoryDown = nil
	a.Proxies = map[string]clash.Proxy{}
	a.GroupNames = nil
	a.Expanded = map[string]bool{}
	a.Connections = []clash.Connection{}
	a.Logs = []LogLine{}
	a.StreamUp = map[string]bool{}
	a.invalidateGroups()
	a.resetBusy()
	a.Message = "disconnected"
	a.Error = ""
}

func (a *App) stopStreams() {
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a *App) resetBusy() {
	a.busy = map[EventKind]bool{}
}

// Refresh reloads every snapshot that is not streamed.
func (a *App) Refresh() {
	a.RefreshProxies()
	a.RefreshRuntime()
	a.RefreshServers()
	a.Message = "refresh requested"
}

// RefreshProxies fetches the proxy and group state.
func (a *App) RefreshProxies() {
	a.poll(EventProxies, func(ctx context.Context, client *clash.Client) (Event, error) {
		proxies, err := client.Proxies(ctx)
		return Event{Proxies: proxies}, err
	})
}

// RefreshRuntime fetches the running configuration.
func (a *App) RefreshRuntime() {
	a.poll(EventConfig, func(ctx context.Context, client *clash.Client) (Event, error) {
		cfg, err := client.Runtime(ctx)
		return Event{Config: &cfg}, err
	})
}

// poll runs a one-shot request unless an identical request is in flight.
func (a *App) poll(kind EventKind, fetch func(context.Context, *clash.Client) (Event, error)) {
	if a.busy[kind] || a.client == nil {
		return
	}
	a.busy[kind] = true
	client := a.client
	gen := a.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		event, err := fetch(ctx, client)
		event.Kind = kind
		event.Gen = gen
		event.Err = err
		a.events <- event
	}()
}

// HandleEvent applies one network result to the dashboard state.
func (a *App) HandleEvent(event Event) {
	// The server monitors are independent of the main connection, so
	// their events bypass the generation gate.
	if event.Kind == EventServer {
		a.handleServer(event)
		return
	}
	if event.Gen != a.gen {
		return
	}
	switch event.Kind {
	case EventVersion:
		a.busy[EventVersion] = false
		a.Connecting = false
		if event.Err != nil {
			a.client = nil
			a.Connected = false
			a.Error = event.Err.Error()
			a.Message = "connection failed; press c in Settings to retry"
			return
		}
		a.client = event.Client
		a.Connected = true
		version := event.Version
		a.Version = version
		a.Error = ""
		a.Message = fmt.Sprintf("connected to mihomo %s", version.Version)
		if err := config.Save(a.Config); err != nil {
			a.Error = "connected, but could not save config: " + err.Error()
		}
		a.startStreams(a.gen)
		a.Refresh()

	case EventConfig:
		a.busy[EventConfig] = false
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		a.Runtime = event.Config

	case EventProxies:
		a.busy[EventProxies] = false
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		a.Proxies = event.Proxies
		a.rebuildGroups()
		a.Error = ""

	case EventTraffic:
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		traffic := event.Traffic
		a.Traffic = *traffic
		a.TrafficUp = true
		a.HistoryUp = appendBounded(a.HistoryUp, maxInt64(traffic.Up, 0), maxHistory)
		a.HistoryDown = appendBounded(a.HistoryDown, maxInt64(traffic.Down, 0), maxHistory)

	case EventMemory:
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		// mihomo reports a deliberate zero for the first sample.
		if *event.Memory == 0 {
			return
		}
		a.Memory = *event.Memory
		a.MemoryUp = true

	case EventConnections:
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		a.applyConnections(event.Snapshot)
		a.Error = ""

	case EventLog:
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		a.appendLog(*event.Log)

	case EventStream:
		a.StreamUp[event.Stream] = event.StreamUp
		if event.StreamUp {
			// A stream that comes back means the controller is reachable
			// again, which is what happens after a kernel restart.
			a.Connected = true
			if a.Error != "" {
				a.Error = ""
			}
			break
		}
		// A single stream may drop on its own, but the controller is
		// really gone once every stream failed.
		for _, up := range a.StreamUp {
			if up {
				return
			}
		}
		a.Connected = false

	case EventAction:
		a.busy[EventAction] = false
		if event.Err != nil {
			a.Error = event.Err.Error()
			return
		}
		a.Error = ""
		a.handleActionResult(event)
	}
}

func (a *App) handleActionResult(event Event) {
	switch event.Action {
	case ActionSelectProxy:
		a.Message = "selected " + event.Node + " in " + event.Group
		a.RefreshProxies()
	case ActionProxyDelay:
		if event.Delay == 0 {
			a.Message = "delay test failed for " + event.Node
		} else {
			a.Message = fmt.Sprintf("%s: %dms", event.Node, event.Delay)
		}
		a.RefreshProxies()
	case ActionGroupDelay:
		a.Message = fmt.Sprintf("tested %d nodes of %s", len(event.Delays), event.Group)
		a.RefreshProxies()
	case ActionSetMode:
		a.Message = "mode set to " + event.Mode
		a.RefreshRuntime()
	case ActionReloadConfig:
		a.Message = "configuration reloaded"
		a.RefreshRuntime()
		a.RefreshProxies()
	case ActionRestart:
		a.Message = "kernel restart requested; waiting for it to come back"
		a.Connected = false
	case ActionFlushDNS:
		a.Message = "dns cache flushed"
	case ActionFlushFakeIP:
		a.Message = "fake-ip cache flushed"
	case ActionCloseConnection:
		a.Message = "connection closed"
		a.RefreshConnections()
	case ActionCloseAll:
		a.Message = "all connections closed"
		a.RefreshConnections()
	}
}

// RefreshConnections asks for a fresh connection snapshot.
func (a *App) RefreshConnections() {
	// The connection stream pushes a new snapshot every second, so an
	// explicit refresh only needs to wait for the next tick.
}

func (a *App) appendLog(entry clash.LogEntry) {
	line := LogLine{
		Time:  time.Now(),
		Level: strings.ToLower(entry.Type),
		Text:  entry.Payload,
	}
	// Every line is buffered. The level filter is applied when the view
	// is rendered, so lowering the filter reveals earlier lines again.
	a.Logs = append(a.Logs, line)
	if len(a.Logs) > maxLogs {
		a.Logs = a.Logs[len(a.Logs)-maxLogs:]
	}
}

// logSeverity maps a mihomo log type to a severity index.
func logSeverity(level string) int {
	if severity, ok := logLevelOrder[strings.ToLower(level)]; ok {
		return severity
	}
	return LogLevelInfo
}

// FilteredLogs returns the log lines matching the current level filter.
func (a *App) FilteredLogs() []LogLine {
	threshold := int(a.LogLevel.Load())
	if threshold == LogLevelAll {
		return a.Logs
	}
	filtered := make([]LogLine, 0, len(a.Logs))
	for _, line := range a.Logs {
		if logSeverity(line.Level) >= threshold {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func (a *App) applyConnections(snapshot *clash.ConnectionsSnapshot) {
	connections := snapshot.Connections
	if len(connections) > maxConns {
		connections = connections[:maxConns]
	}
	a.Connections = connections
	a.sortConnections()
	a.clampConnCursor()
}

func (a *App) sortConnections() {
	sort.SliceStable(a.Connections, func(i, j int) bool {
		left, right := a.Connections[i], a.Connections[j]
		switch a.ConnSort {
		case ConnSortDownload:
			return left.Download > right.Download
		case ConnSortUpload:
			return left.Upload > right.Upload
		default:
			return left.Start.After(right.Start)
		}
	})
}

func (a *App) clampConnCursor() {
	if len(a.Connections) == 0 {
		a.ConnCursor = 0
		return
	}
	if a.ConnCursor >= len(a.Connections) {
		a.ConnCursor = len(a.Connections) - 1
	}
	if a.ConnCursor < 0 {
		a.ConnCursor = 0
	}
}

// rebuildGroups recomputes the sorted group list and the flattened tree.
func (a *App) rebuildGroups() {
	names := make([]string, 0)
	for name, proxy := range a.Proxies {
		if proxy.IsGroup() && !proxy.Hidden {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	a.GroupNames = names
	a.invalidateGroups()
	a.clampGroupCursor()
}

// invalidateGroups drops the cached tree so it is rebuilt on demand.
func (a *App) invalidateGroups() {
	a.GroupRowsOK = false
}

// Rows returns the flattened proxy group tree.
func (a *App) Rows() []GroupRow {
	if a.GroupRowsOK {
		return a.GroupRows
	}
	rows := make([]GroupRow, 0, len(a.GroupNames))
	for groupIndex, name := range a.GroupNames {
		rows = append(rows, GroupRow{GroupIndex: groupIndex})
		if !a.GroupExpanded(name) {
			continue
		}
		group := a.Proxies[name]
		for itemIndex := range group.All {
			rows = append(rows, GroupRow{GroupIndex: groupIndex, ItemIndex: itemIndex, IsItem: true})
		}
	}
	a.GroupRows = rows
	a.GroupRowsOK = true
	return rows
}

// groupExpanded reports whether a group tree node is expanded.
//
// The server does not persist the expand state, so the dashboard keeps
// its own toggle and only falls back to the server value.
func (a *App) GroupExpanded(name string) bool {
	if expanded, ok := a.Expanded[name]; ok {
		return expanded
	}
	return a.Proxies[name].IsGroup()
}

// ToggleGroup flips the expand state of a group.
func (a *App) ToggleGroup(name string) {
	a.Expanded[name] = !a.GroupExpanded(name)
	a.invalidateGroups()
	a.clampGroupCursor()
}

func (a *App) clampGroupCursor() {
	count := len(a.Rows())
	if count == 0 {
		a.GroupCursor = 0
		return
	}
	if a.GroupCursor >= count {
		a.GroupCursor = count - 1
	}
	if a.GroupCursor < 0 {
		a.GroupCursor = 0
	}
}

// SelectedGroupRow returns the row under the cursor.
func (a *App) SelectedGroupRow() (GroupRow, bool) {
	rows := a.Rows()
	if a.GroupCursor < 0 || a.GroupCursor >= len(rows) {
		return GroupRow{}, false
	}
	return rows[a.GroupCursor], true
}

// SelectedConnection returns the connection under the cursor.
func (a *App) SelectedConnection() (clash.Connection, bool) {
	if a.ConnCursor < 0 || a.ConnCursor >= len(a.Connections) {
		return clash.Connection{}, false
	}
	return a.Connections[a.ConnCursor], true
}

// Mode returns the running tunnel mode.
func (a *App) Mode() string {
	if a.Runtime == nil || a.Runtime.Mode == "" {
		return "-"
	}
	return a.Runtime.Mode
}

// LogLevelName returns the name of the active log filter.
func (a *App) LogLevelName() string {
	return logLevelNames[a.LogLevel.Load()]
}

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
	if event.Key() == tcell.KeyCtrlC || event.Rune() == 'q' {
		a.ShouldQuit = true
		return
	}
	if event.Rune() == '?' {
		a.Help = true
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
		a.GroupCursor = maxInt(a.GroupCursor-1, 0)
	case tcell.KeyDown:
		count := len(a.Rows())
		if count > 0 {
			a.GroupCursor = minInt(a.GroupCursor+1, count-1)
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
		a.ConnCursor = maxInt(a.ConnCursor-1, 0)
	case tcell.KeyDown:
		a.ConnCursor = minInt(a.ConnCursor+1, maxInt(len(a.Connections)-1, 0))
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
		a.LogOffset = maxInt(a.LogOffset-10, 0)
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

func (a *App) handleServersKey(event *tcell.EventKey) {
	page := maxInt(a.ServersPageSize, 1)
	count := len(a.Servers)
	switch event.Key() {
	case tcell.KeyUp:
		a.ServerCursor = maxInt(a.ServerCursor-1, 0)
	case tcell.KeyDown:
		if count > 0 {
			a.ServerCursor = minInt(a.ServerCursor+1, count-1)
		}
	case tcell.KeyPgUp:
		a.ServerCursor = maxInt(a.ServerCursor-page, 0)
	case tcell.KeyPgDn:
		if count > 0 {
			a.ServerCursor = minInt(a.ServerCursor+page, count-1)
		}
	case tcell.KeyHome:
		a.ServerCursor = 0
	case tcell.KeyEnd:
		if count > 0 {
			a.ServerCursor = count - 1
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

// logQuery returns the server side log level filter.
//
// mihomo skips every record whose level is below the requested one, so
// "debug" is the value that forwards everything the kernel produced.
func logQuery() url.Values {
	return url.Values{"level": []string{"debug"}}
}
