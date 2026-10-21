package singbox

import "time"

// Connection is one tracked connection of a sing-box instance.
type Connection struct {
	ID            string
	Inbound       string
	InboundType   string
	Network       string
	Source        string
	Destination   string
	Domain        string
	Protocol      string
	CreatedAt     int64
	ClosedAt      int64
	Uplink        int64
	Downlink      int64
	UplinkTotal   int64
	DownlinkTotal int64
	Rule          string
	Outbound      string
	OutboundType  string
	ChainList     []string
}

// ConnectionEvent is one change of a tracked connection.
//
// The type follows the enum of the sing-box API:
//
//	0 = new
//	1 = update
//	2 = closed
type ConnectionEvent struct {
	Type          int32
	ID            string
	Connection    *Connection
	UplinkDelta   int64
	DownlinkDelta int64
	ClosedAt      int64
}

// ConnectionEvents is one payload of the connection stream.
type ConnectionEvents struct {
	Events []ConnectionEvent
	Reset  bool
}

// LogEntry is one log record of a sing-box instance.
//
// The level follows the enum of the sing-box API, where a higher value
// means a more verbose record:
//
//	0 = panic, 1 = fatal, 2 = error, 3 = warn, 4 = info, 5 = debug, 6 = trace
type LogEntry struct {
	Level   int32
	Message string
}

// Log is one payload of the log stream.
type Log struct {
	Messages []LogEntry
	Reset    bool
}

// LogLevelName maps a level of the sing-box API to its name.
func LogLevelName(level int32) string {
	switch level {
	case 0:
		return "panic"
	case 1:
		return "fatal"
	case 2:
		return "error"
	case 3:
		return "warn"
	case 4:
		return "info"
	case 5:
		return "debug"
	case 6:
		return "trace"
	default:
		return "?"
	}
}

// CreatedTime returns the start of the connection.
func (c Connection) CreatedTime() time.Time {
	if c.CreatedAt <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(c.CreatedAt)
}
