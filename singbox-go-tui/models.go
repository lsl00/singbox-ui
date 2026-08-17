package main

type VersionInfo struct {
	Version    string
	APIVersion int32
}

type ServiceStatusInfo struct {
	Status       int32
	ErrorMessage string
}

type StatusInfo struct {
	Memory           uint64
	Goroutines       int32
	ConnectionsIn    int32
	ConnectionsOut   int32
	TrafficAvailable bool
	Uplink           int64
	Downlink         int64
	UplinkTotal      int64
	DownlinkTotal    int64
}

type GroupInfo struct {
	Tag        string
	GroupType  string
	Selectable bool
	Selected   string
	IsExpand   bool
	Items      []GroupItemInfo
}

type GroupItemInfo struct {
	Tag          string
	ItemType     string
	URLTestTime  int64
	URLTestDelay int32
}

type ClashModeInfo struct {
	ModeList    []string
	CurrentMode string
}

type ConnectionInfo struct {
	ID            string
	Inbound       string
	InboundType   string
	IPVersion     int32
	Network       string
	Source        string
	Destination   string
	Domain        string
	Protocol      string
	User          string
	FromOutbound  string
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

type ConnectionEventInfo struct {
	EventType     int32
	ID            string
	Connection    *ConnectionInfo
	UplinkDelta   int64
	DownlinkDelta int64
	ClosedAt      int64
}

type ConnectionEventsInfo struct {
	Events []ConnectionEventInfo
	Reset  bool
}

type LogEntryInfo struct {
	Level   int32
	Message string
}

type LogsInfo struct {
	Messages []LogEntryInfo
	Reset    bool
}

type ConnectionRow struct {
	Connection   ConnectionInfo
	UplinkRate   int64
	DownlinkRate int64
	ClosedAt     int64
	Closed       bool
}

type LogRow struct {
	Level   int32
	Message string
}
