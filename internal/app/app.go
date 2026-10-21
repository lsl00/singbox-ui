// Package app holds the state of the dashboard and the logic that
// reacts to the keys and to the results of the network requests.
package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

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

// logLevelNames lists the levels of the filter in the order the cycle
// walks through them.
var logLevelNames = []string{"all", "info", "warning", "error"}

// logSeverity maps a kernel log level to its severity.
var logSeverity = map[string]int{"debug": 0, "info": 1, "warning": 2, "error": 3}

// ConnSort selects the ordering of the connections table.
type ConnSort int

// The available connection orderings.
const (
	ConnSortRecent ConnSort = iota
	ConnSortDownload
	ConnSortUpload
)

// Label returns the name of the ordering.
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

	Proxies     map[string]clash.Proxy
	GroupNames  []string
	Expanded    map[string]bool
	GroupCursor int
	GroupRows   []GroupRow
	GroupRowsOK bool

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
		a.HistoryUp = appendBounded(a.HistoryUp, max(traffic.Up, 0), maxHistory)
		a.HistoryDown = appendBounded(a.HistoryDown, max(traffic.Down, 0), maxHistory)

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
			a.Error = ""
			break
		}
		// A single stream may drop on its own, but the controller is
		// really gone once every stream failed.
		if !anyStreamUp(a.StreamUp) {
			a.Connected = false
		}

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
		// The connection stream pushes a new snapshot every second, so
		// the closed connection disappears on its own.
		a.Message = "connection closed"
	case ActionCloseAll:
		a.Message = "all connections closed"
	}
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

// severityOf returns the severity of a kernel log level. An unknown level
// is treated as info.
func severityOf(level string) int {
	if severity, ok := logSeverity[strings.ToLower(level)]; ok {
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
		if severityOf(line.Level) >= threshold {
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

// anyStreamUp reports whether at least one stream is still connected.
func anyStreamUp(streams map[string]bool) bool {
	for _, up := range streams {
		if up {
			return true
		}
	}
	return false
}

// appendBounded appends a value and keeps only the last limit values.
func appendBounded(values []int64, value int64, limit int) []int64 {
	values = append(values, value)
	if len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values
}
