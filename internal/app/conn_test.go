package app

import (
	"strings"
	"testing"
	"time"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
	"mihomo-tui/internal/singbox"
)

func TestConnFromSingBox(t *testing.T) {
	connection := singbox.Connection{
		ID:          "abc",
		Source:      "10.0.0.1:1234",
		Destination: "example.com:443",
		Protocol:    "tcp",
		Network:     "tcp",
		ChainList:   []string{"proxy", "node"},
		Rule:        "DomainSuffix",
		CreatedAt:   1700000000000,
	}
	info := connFromSingBox(connection)
	if info.Destination != "example.com" {
		t.Fatalf("unexpected destination: %q", info.Destination)
	}
	if info.Source != "10.0.0.1:1234" {
		t.Fatalf("unexpected source: %q", info.Source)
	}
	if info.CreatedAt.IsZero() {
		t.Fatal("the start of the connection is missing")
	}
	if len(info.Chain) != 2 {
		t.Fatalf("unexpected chain: %+v", info.Chain)
	}
}

func TestStripPort(t *testing.T) {
	cases := map[string]string{
		"example.com:443": "example.com",
		"1.2.3.4:80":      "1.2.3.4",
		"[::1]:443":       "[::1]",
		"example.com":     "example.com",
		"host:":           "host:",
		":443":            ":443",
	}
	for input, want := range cases {
		if got := stripPort(input); got != want {
			t.Fatalf("stripPort(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConnSetAppliesEvents(t *testing.T) {
	set := connSet{}
	set.apply(connEvent{Kind: connNew, Info: ConnInfo{ID: "a", Destination: "one.example"}})
	set.apply(connEvent{Kind: connNew, Info: ConnInfo{ID: "b", Destination: "two.example"}})
	if rows := set.list(); len(rows) != 2 {
		t.Fatalf("unexpected rows: %+v", rows)
	}

	// An update carries the difference to the previous update, so the
	// values accumulate.
	set.apply(connEvent{Kind: connUpdate, Info: ConnInfo{ID: "a"}, Uploaded: 100, Downloaded: 200})
	rows := set.list()
	if rows[0].ID != "a" || rows[0].Upload != 100 || rows[0].Download != 200 {
		t.Fatalf("the update was not applied: %+v", rows[0])
	}
	set.apply(connEvent{Kind: connUpdate, Info: ConnInfo{ID: "a"}, Uploaded: 50, Downloaded: 25})
	rows = set.list()
	if rows[0].Upload != 150 || rows[0].Download != 225 {
		t.Fatalf("the update did not accumulate: %+v", rows[0])
	}
	if rows[0].Destination != "one.example" {
		t.Fatalf("the update dropped the destination: %+v", rows[0])
	}

	// A closed connection keeps its row but stops transferring.
	set.apply(connEvent{Kind: connClosed, Info: ConnInfo{ID: "a"}})
	rows = set.list()
	if !rows[0].Closed || rows[0].Upload != 0 {
		t.Fatalf("the close was not applied: %+v", rows[0])
	}

	// A reset drops everything.
	set.reset()
	if rows := set.list(); len(rows) != 0 {
		t.Fatalf("the reset was not applied: %+v", rows)
	}
}

func TestConnSetIgnoresUnknownClose(t *testing.T) {
	set := connSet{}
	set.apply(connEvent{Kind: connClosed, Info: ConnInfo{ID: "missing"}})
	if rows := set.list(); len(rows) != 0 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestTheMainPanelKeepsTheLocalConnections(t *testing.T) {
	state := newTestApp()
	state.Connections = []ConnInfo{{ID: "local"}}

	// Opening an instance must not change the connections of the local
	// kernel, the overlay shows the remote ones.
	state.OpenedServer = 0
	state.serverConns.apply(connEvent{Kind: connNew, Info: ConnInfo{ID: "remote"}})

	if rows := state.Connections; len(rows) != 1 || rows[0].ID != "local" {
		t.Fatalf("the local connections changed: %+v", rows)
	}
	if rows := state.ServerConnections(); len(rows) != 1 || rows[0].ID != "remote" {
		t.Fatalf("unexpected server connections: %+v", rows)
	}
}

func TestTheMainPanelKeepsTheLocalLogs(t *testing.T) {
	state := newTestApp()
	state.Logs = []LogLine{{Text: "local"}}

	state.OpenedServer = 0
	state.serverLogs = []LogLine{{Text: "remote"}}

	if rows := state.Logs; len(rows) != 1 || rows[0].Text != "local" {
		t.Fatalf("the local log records changed: %+v", rows)
	}
	if rows := state.ServerFilteredLogs(); len(rows) != 1 || rows[0].Text != "remote" {
		t.Fatalf("unexpected server log records: %+v", rows)
	}
}

// entryNamed builds a server entry for the tests.
func entryNamed(name string) config.ServerEntry {
	return config.ServerEntry{Name: name, BaseURL: "http://127.0.0.1:9000", Protocol: config.ProtocolSingBox}
}

func TestOverlayTitleNamesTheInstance(t *testing.T) {
	state := newTestApp()
	if state.OverlayTitle() != "" {
		t.Fatalf("unexpected title: %q", state.OverlayTitle())
	}
	state.OpenedServer = 0
	state.Servers = []ServerMonitor{{Entry: entryNamed("hk")}}
	title := state.OverlayTitle()
	if !strings.Contains(title, "hk") {
		t.Fatalf("unexpected title: %q", title)
	}
}

func TestOpenServerNeedsAProbe(t *testing.T) {
	state := newTestApp()
	state.Servers = []ServerMonitor{{Entry: entryNamed("broken")}}
	state.OpenServer(0)
	if state.OpenedServer != -1 {
		t.Fatalf("the instance must not open without a probe")
	}
	if state.Message == "" {
		t.Fatal("the failure must be visible")
	}
}

func TestCloseServerResetsTheState(t *testing.T) {
	state := newTestApp()
	state.OpenedServer = 0
	state.serverConns.apply(connEvent{Kind: connNew, Info: ConnInfo{ID: "a"}})
	state.serverLogs = []LogLine{{Text: "x"}}
	state.CloseServer()
	if state.OpenedServer != -1 {
		t.Fatalf("the instance is still open")
	}
	if rows := state.ServerConnections(); len(rows) != 0 {
		t.Fatalf("the connections were not dropped: %+v", rows)
	}
	if rows := state.ServerFilteredLogs(); len(rows) != 0 {
		t.Fatalf("the log records were not dropped: %+v", rows)
	}
}

func TestSingBoxLogLevelMapping(t *testing.T) {
	cases := map[int32]string{
		0: "error",
		1: "error",
		2: "error",
		3: "warning",
		4: "info",
		5: "debug",
		6: "debug",
	}
	for level, want := range cases {
		if got := singboxLogLevel(level); got != want {
			t.Fatalf("singboxLogLevel(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestConnFromClashKeepsTheDetails(t *testing.T) {
	connection := clash.Connection{
		ID:       "abc",
		Metadata: clash.Metadata{Network: "tcp", Type: "HTTP", Host: "example.com", SourceIP: "10.0.0.1", SourcePort: "1234", DestinationIP: "1.2.3.4", DestinationPort: "443", DNSMode: "fake-ip", InboundName: "mixed"},
		Upload:   10,
		Download: 20,
		Start:    time.Now(),
		Chains:   []string{"proxy"},
		Rule:     "DomainSuffix",
	}
	info := connFromClash(connection)
	if info.Destination != "example.com" || info.Source != "10.0.0.1:1234" {
		t.Fatalf("unexpected endpoints: %+v", info)
	}
	if info.Inbound != "mixed" || info.DNSMode != "fake-ip" {
		t.Fatalf("unexpected metadata: %+v", info)
	}
}
