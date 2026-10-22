package app

import (
	"sort"
	"strings"
	"time"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/singbox"
)

// ConnInfo is the unified view of a tracked connection.
//
// The dashboard shows the connections of the local kernel and of a
// monitored instance with the same table, so both are mapped to this
// model.
type ConnInfo struct {
	ID          string
	Source      string
	Destination string
	Protocol    string
	Network     string
	Rule        string
	RulePayload string
	Chain       []string
	Upload      int64
	Download    int64
	CreatedAt   time.Time
	Inbound     string
	InboundType string
	Process     string
	DNSMode     string
	Closed      bool
}

// connFromClash maps a connection of the mihomo API.
func connFromClash(c clash.Connection) ConnInfo {
	return ConnInfo{
		ID:          c.ID,
		Source:      c.Source(),
		Destination: c.Destination(),
		Protocol:    c.Metadata.Type,
		Network:     c.Metadata.Network,
		Rule:        c.Rule,
		RulePayload: c.RulePayload,
		Chain:       c.Chains,
		Upload:      c.Upload,
		Download:    c.Download,
		CreatedAt:   c.Start,
		Inbound:     c.Metadata.InboundName,
		DNSMode:     c.Metadata.DNSMode,
	}
}

// connFromSingBox maps a connection of the sing-box API.
func connFromSingBox(c singbox.Connection) ConnInfo {
	destination := c.Domain
	if destination == "" {
		destination = stripPort(c.Destination)
	}
	if destination == "" {
		destination = "-"
	}
	return ConnInfo{
		ID:          c.ID,
		Source:      c.Source,
		Destination: destination,
		Protocol:    c.Protocol,
		Network:     c.Network,
		Rule:        c.Rule,
		Chain:       c.ChainList,
		Upload:      c.Uplink,
		Download:    c.Downlink,
		CreatedAt:   c.CreatedTime(),
		Inbound:     c.Inbound,
		InboundType: c.InboundType,
		Process:     processOf(c.Process),
	}
}

// processOf renders the process of a connection of a sing-box instance.
func processOf(info *singbox.ProcessInfo) string {
	if info == nil {
		return ""
	}
	if info.ProcessPath != "" {
		return info.ProcessPath
	}
	return info.PackageName
}

// stripPort removes the port of an address of the form host:port.
func stripPort(address string) string {
	index := strings.LastIndex(address, ":")
	if index <= 0 || index == len(address)-1 {
		return address
	}
	for _, r := range address[index+1:] {
		if r < '0' || r > '9' {
			return address
		}
	}
	return address[:index]
}

// The kinds of a connection event.
const (
	connNew = iota
	connUpdate
	connClosed
)

// connEvent is one change of a tracked connection.
type connEvent struct {
	Kind       int
	Info       ConnInfo
	Uploaded   int64
	Downloaded int64
	ClosedAt   time.Time
}

// connBatch is a set of connection changes.
type connBatch struct {
	Reset  bool
	Events []connEvent
}

// logBatch is a set of log records.
type logBatch struct {
	Reset   bool
	Entries []LogLine
}

// connSet keeps the connections of a monitored instance.
//
// The sing-box API reports the connections as events, so the set has to
// merge them into the current state.
type connSet struct {
	rows map[string]ConnInfo
}

// reset drops all connections.
func (s *connSet) reset() {
	s.rows = map[string]ConnInfo{}
}

// apply merges one change into the set.
func (s *connSet) apply(event connEvent) {
	if s.rows == nil {
		s.rows = map[string]ConnInfo{}
	}
	switch event.Kind {
	case connNew:
		s.rows[event.Info.ID] = event.Info
	case connUpdate:
		row, ok := s.rows[event.Info.ID]
		if !ok {
			if event.Info.ID == "" {
				return
			}
			row = event.Info
		}
		// The sing-box API reports the difference to the previous
		// update, so the values accumulate.
		row.Upload += event.Uploaded
		row.Download += event.Downloaded
		if event.Info.ID != "" {
			row.ID = event.Info.ID
		}
		if event.Info.Destination != "" {
			row.Destination = event.Info.Destination
		}
		if event.Info.Protocol != "" {
			row.Protocol = event.Info.Protocol
		}
		if event.Info.Network != "" {
			row.Network = event.Info.Network
		}
		if event.Info.Rule != "" {
			row.Rule = event.Info.Rule
		}
		if len(event.Info.Chain) > 0 {
			row.Chain = event.Info.Chain
		}
		if !event.Info.CreatedAt.IsZero() {
			row.CreatedAt = event.Info.CreatedAt
		}
		s.rows[event.Info.ID] = row
	case connClosed:
		row, ok := s.rows[event.Info.ID]
		if !ok {
			return
		}
		row.Upload = 0
		row.Download = 0
		row.Closed = true
		s.rows[event.Info.ID] = row
	}
}

// applyConnBatch merges the changes of a monitored instance.
func (a *App) applyConnBatch(batch connBatch) {
	if batch.Reset {
		a.serverConns.reset()
	}
	for _, event := range batch.Events {
		a.serverConns.apply(event)
	}
	a.clampConnCursor()
}

// list returns the connections ordered by their identifier.
func (s *connSet) list() []ConnInfo {
	rows := make([]ConnInfo, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ID < rows[j].ID
	})
	return rows
}
