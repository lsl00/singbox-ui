package app

import (
	"mihomo-tui/internal/clash"
)

// View identifies one page of the dashboard.
type View int

// The available pages. The order defines the numeric page shortcuts.
const (
	ViewOverview View = iota
	ViewGroups
	ViewConnections
	ViewLogs
	ViewSettings
	ViewServers
)

// ViewCount is the number of pages.
const ViewCount = 6

// Title returns the page title shown in the tab bar.
func (v View) Title() string {
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

// Number returns the one based shortcut of the page.
func (v View) Number() int {
	return int(v) + 1
}

// EventKind classifies a message coming from a network goroutine.
type EventKind int

// The event kinds understood by the main loop.
const (
	EventVersion EventKind = iota
	EventConfig
	EventProxies
	EventTraffic
	EventMemory
	EventConnections
	EventLog
	EventAction
	EventStream
	EventServer
	EventServerConnections
	EventServerLog
	EventServerError
)

// ActionKind classifies a write operation the user triggered.
type ActionKind int

// The write operations the dashboard can perform.
const (
	ActionSelectProxy ActionKind = iota
	ActionProxyDelay
	ActionGroupDelay
	ActionSetMode
	ActionReloadConfig
	ActionRestart
	ActionFlushDNS
	ActionFlushFakeIP
	ActionCloseConnection
	ActionCloseAll
)

// Event is a single message produced by a network goroutine.
//
// Gen carries the connection generation the event belongs to. Events of an
// outdated generation are dropped, which keeps late responses of a previous
// controller from overwriting the current state.
type Event struct {
	Kind EventKind
	Gen  uint64
	Err  error

	Client  *clash.Client
	Version *clash.Version
	Config  *clash.Runtime
	Proxies map[string]clash.Proxy

	Traffic  *clash.Traffic
	Memory   *uint64
	Snapshot *clash.ConnectionsSnapshot
	Log      *clash.LogEntry

	Action ActionKind
	Mode   string
	Group  string
	Node   string

	Delays map[string]uint16
	Delay  uint16

	Stream   string
	StreamUp bool

	ServerIndex int
	ServerInfo  *ServerInfo

	// Batch carries the changes of a monitored instance, either the
	// connections or the log records.
	ConnBatch *connBatch
	LogBatch  *logBatch
}

// ServerInfo is the snapshot of one monitored instance.
type ServerInfo struct {
	Version    string
	Uplink     int64
	Downlink   int64
	UpTotal    int64
	DownTotal  int64
	Memory     uint64
	Conns      int
	Goroutines int32
}
