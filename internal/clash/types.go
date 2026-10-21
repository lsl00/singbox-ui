package clash

import "time"

// Version is the payload of GET /version.
type Version struct {
	Version string `json:"version"`
}

// Traffic is one sample of the /traffic stream. Up and Down are byte
// rates per second, UpTotal and DownTotal are cumulative byte counters.
type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

// Memory is one sample of the /memory stream. Inuse is the heap size in
// bytes.
type Memory struct {
	Inuse uint64 `json:"inuse"`
}

// DelayHistory is one latency sample of a proxy. A delay of zero means the
// node did not answer.
type DelayHistory struct {
	Delay uint16 `json:"delay"`
}

// Proxy describes either a single outbound node or a policy group.
//
// mihomo serialises groups with extra keys (now, all, testUrl...) while
// plain nodes only carry the common keys, so a proxy is a group exactly
// when All is populated.
type Proxy struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	TestURL string         `json:"testUrl"`
	Hidden  bool           `json:"hidden"`
	History []DelayHistory `json:"history"`
}

// IsGroup reports whether the proxy is a policy group.
func (p Proxy) IsGroup() bool {
	return len(p.All) > 0
}

// IsSelector reports whether the group lets the user pick a node.
func (p Proxy) IsSelector() bool {
	return p.Type == "Selector"
}

// LastDelay returns the most recent measured latency in milliseconds.
// The second return value is false when the node was never measured.
func (p Proxy) LastDelay() (uint16, bool) {
	if len(p.History) == 0 {
		return 0, false
	}
	return p.History[len(p.History)-1].Delay, true
}

// Runtime is the part of GET /configs the dashboard displays.
type Runtime struct {
	Mode     string `json:"mode"`
	LogLevel string `json:"log-level"`
}

// Metadata describes the endpoints of one connection.
type Metadata struct {
	Network         string `json:"network"`
	Type            string `json:"type"`
	SourceIP        string `json:"sourceIP"`
	SourcePort      string `json:"sourcePort"`
	DestinationIP   string `json:"destinationIP"`
	DestinationPort string `json:"destinationPort"`
	Host            string `json:"host"`
	DNSMode         string `json:"dnsMode"`
	InboundName     string `json:"inboundName"`
}

// Connection is one tracked connection.
type Connection struct {
	ID          string    `json:"id"`
	Metadata    Metadata  `json:"metadata"`
	Upload      int64     `json:"upload"`
	Download    int64     `json:"download"`
	Start       time.Time `json:"start"`
	Chains      []string  `json:"chains"`
	Rule        string    `json:"rule"`
	RulePayload string    `json:"rulePayload"`
}

// Destination renders the best available human readable target.
func (c Connection) Destination() string {
	if c.Metadata.Host != "" {
		return c.Metadata.Host
	}
	if c.Metadata.DestinationIP != "" {
		return c.Metadata.DestinationIP
	}
	return "-"
}

// Source renders the client address of the connection.
func (c Connection) Source() string {
	ip := c.Metadata.SourceIP
	if ip == "" {
		return "-"
	}
	if c.Metadata.SourcePort != "" && c.Metadata.SourcePort != "0" {
		return ip + ":" + c.Metadata.SourcePort
	}
	return ip
}

// Chain renders the proxy chain the connection travelled through.
func (c Connection) Chain() string {
	if len(c.Chains) == 0 {
		return "-"
	}
	out := ""
	for index, name := range c.Chains {
		if index > 0 {
			out += " -> "
		}
		out += name
	}
	return out
}

// ConnectionsSnapshot is one payload of the /connections stream.
type ConnectionsSnapshot struct {
	DownloadTotal int64        `json:"downloadTotal"`
	UploadTotal   int64        `json:"uploadTotal"`
	Memory        uint64       `json:"memory"`
	Connections   []Connection `json:"connections"`
}

// LogEntry is one line of the /logs stream.
type LogEntry struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}
