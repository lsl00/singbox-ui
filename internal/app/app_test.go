package app

import (
	"context"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
)

func newTestApp() *App {
	return New(config.APIConfig{URL: config.DefaultAPIURL})
}

func TestLogFilterKeepsSeverityOrder(t *testing.T) {
	state := newTestApp()
	state.appendLog(clash.LogEntry{Type: "debug", Payload: "d"})
	state.appendLog(clash.LogEntry{Type: "info", Payload: "i"})
	state.appendLog(clash.LogEntry{Type: "warning", Payload: "w"})
	state.appendLog(clash.LogEntry{Type: "error", Payload: "e"})

	if got := len(state.FilteredLogs()); got != 4 {
		t.Fatalf("expected every line with the default filter, got %d", got)
	}
	state.LogLevel.Store(LogLevelInfo)
	if got := len(state.FilteredLogs()); got != 3 {
		t.Fatalf("expected info, warning and error, got %d", got)
	}
	state.LogLevel.Store(LogLevelWarning)
	if got := len(state.FilteredLogs()); got != 2 {
		t.Fatalf("expected warning and error, got %d", got)
	}
	state.LogLevel.Store(LogLevelError)
	lines := state.FilteredLogs()
	if len(lines) != 1 || lines[0].Text != "e" {
		t.Fatalf("expected only the error line, got %+v", lines)
	}
	// Lowering the filter again must reveal the buffered lines.
	state.LogLevel.Store(LogLevelAll)
	if got := len(state.FilteredLogs()); got != 4 {
		t.Fatalf("expected the whole buffer again, got %d", got)
	}
}

func TestLogBufferIsBounded(t *testing.T) {
	state := newTestApp()
	for i := 0; i < maxLogs+50; i++ {
		state.appendLog(clash.LogEntry{Type: "info", Payload: "line"})
	}
	if len(state.Logs) != maxLogs {
		t.Fatalf("expected the buffer to be capped at %d, got %d", maxLogs, len(state.Logs))
	}
}

func TestGroupTreeAndExpansion(t *testing.T) {
	state := newTestApp()
	state.Proxies = map[string]clash.Proxy{
		"Selector": {Name: "Selector", Type: "Selector", All: []string{"Node A", "Node B"}, Now: "Node A"},
		"Node A":   {Name: "Node A", Type: "Shadowsocks"},
		"Node B":   {Name: "Node B", Type: "VMess"},
	}
	state.rebuildGroups()

	if len(state.GroupNames) != 1 || state.GroupNames[0] != "Selector" {
		t.Fatalf("unexpected group list: %+v", state.GroupNames)
	}
	rows := state.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected the group plus two nodes, got %d rows", len(rows))
	}
	if !rows[1].IsItem || rows[1].ItemIndex != 0 {
		t.Fatalf("unexpected first item row: %+v", rows[1])
	}

	state.ToggleGroup("Selector")
	rows = state.Rows()
	if len(rows) != 1 || rows[0].IsItem {
		t.Fatalf("expected the group to be collapsed, got %+v", rows)
	}
	state.ToggleGroup("Selector")
	if len(state.Rows()) != 3 {
		t.Fatalf("expected the group to be expanded again")
	}
}

func TestHiddenGroupsAreSkipped(t *testing.T) {
	state := newTestApp()
	state.Proxies = map[string]clash.Proxy{
		"Visible": {Name: "Visible", Type: "Selector", All: []string{"Node A"}},
		"Hidden":  {Name: "Hidden", Type: "Selector", All: []string{"Node B"}, Hidden: true},
	}
	state.rebuildGroups()
	if len(state.GroupNames) != 1 || state.GroupNames[0] != "Visible" {
		t.Fatalf("unexpected group list: %+v", state.GroupNames)
	}
}

func TestStaleEventsAreDropped(t *testing.T) {
	state := newTestApp()
	state.gen = 7
	state.Proxies = map[string]clash.Proxy{"Old": {Name: "Old"}}

	state.HandleEvent(Event{
		Kind:    EventProxies,
		Gen:     6,
		Proxies: map[string]clash.Proxy{"Stale": {Name: "Stale"}},
	})
	if _, ok := state.Proxies["Stale"]; ok {
		t.Fatalf("a stale event must not overwrite the current state")
	}

	state.HandleEvent(Event{
		Kind:    EventProxies,
		Gen:     7,
		Proxies: map[string]clash.Proxy{"Fresh": {Name: "Fresh"}},
	})
	if _, ok := state.Proxies["Fresh"]; !ok {
		t.Fatalf("a current event must be applied")
	}
}

func TestActionIsRejectedWhileBusy(t *testing.T) {
	state := newTestApp()
	client, err := clash.New(clash.Config{URL: "http://127.0.0.1:9090"})
	if err != nil {
		t.Fatal(err)
	}
	state.client = client
	state.busy[EventAction] = true
	state.Message = ""

	called := false
	state.runAction(ActionSelectProxy, "", func(ctx context.Context, client *clash.Client) (Event, error) {
		called = true
		return Event{}, nil
	})
	if called {
		t.Fatalf("the action must not run while another one is in flight")
	}
	if state.Message != "previous action still in progress" {
		t.Fatalf("unexpected message: %q", state.Message)
	}
}

func TestConnectionsAreSortedBySelectedKey(t *testing.T) {
	state := newTestApp()
	base := time.Now()
	state.Connections = []clash.Connection{
		{ID: "a", Upload: 10, Download: 5, Start: base.Add(-2 * time.Minute)},
		{ID: "b", Upload: 30, Download: 50, Start: base},
		{ID: "c", Upload: 20, Download: 20, Start: base.Add(-time.Minute)},
	}

	state.ConnSort = ConnSortRecent
	state.sortConnections()
	if state.Connections[0].ID != "b" {
		t.Fatalf("expected the newest connection first, got %s", state.Connections[0].ID)
	}
	state.ConnSort = ConnSortDownload
	state.sortConnections()
	if state.Connections[0].ID != "b" {
		t.Fatalf("expected the largest download first, got %s", state.Connections[0].ID)
	}
	state.ConnSort = ConnSortUpload
	state.sortConnections()
	if state.Connections[0].ID != "b" {
		t.Fatalf("expected the largest upload first, got %s", state.Connections[0].ID)
	}
}

func TestConnectionSnapshotIsApplied(t *testing.T) {
	state := newTestApp()
	snapshot := &clash.ConnectionsSnapshot{
		DownloadTotal: 10,
		UploadTotal:   5,
		Connections: []clash.Connection{
			{ID: "a", Upload: 1},
			{ID: "b", Upload: 2},
		},
	}
	state.HandleEvent(Event{Kind: EventConnections, Gen: state.gen, Snapshot: snapshot})
	if len(state.Connections) != 2 {
		t.Fatalf("expected two connections, got %d", len(state.Connections))
	}
}

func TestModeFallsBackToPlaceholder(t *testing.T) {
	state := newTestApp()
	if state.Mode() != "-" {
		t.Fatalf("unexpected mode: %q", state.Mode())
	}
	state.Runtime = &clash.Runtime{Mode: "global"}
	if state.Mode() != "global" {
		t.Fatalf("unexpected mode: %q", state.Mode())
	}
}

func TestModeCyclingWrapsAround(t *testing.T) {
	controller := newFakeController(t)
	client, err := clash.New(clash.Config{URL: controller.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestApp()
	state.client = client
	state.Runtime = &clash.Runtime{Mode: "direct"}

	state.cycleMode(1)
	waitForAction(t, state)
	if state.Message != "mode set to rule" {
		t.Fatalf("unexpected message: %q", state.Message)
	}
	state.Runtime = &clash.Runtime{Mode: "rule"}
	state.cycleMode(-1)
	waitForAction(t, state)
	if state.Message != "mode set to direct" {
		t.Fatalf("unexpected message: %q", state.Message)
	}
}

// waitForAction pumps the dashboard until the running action finished.
func waitForAction(t *testing.T, state *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case event := <-state.Events():
			state.HandleEvent(event)
			if event.Kind == EventAction {
				return
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("the action did not finish in time")
}

func TestSettingsEditingRoundTrip(t *testing.T) {
	state := newTestApp()
	state.SettingsField = 0
	state.beginSettingsEdit()
	state.setSettingsValue("http://127.0.0.1:9999")
	state.SettingsEditing = false
	if state.Config.URL != "http://127.0.0.1:9999" {
		t.Fatalf("unexpected url: %q", state.Config.URL)
	}

	state.SettingsField = 1
	state.beginSettingsEdit()
	state.SettingsOriginal = state.settingsValue()
	state.setSettingsValue("changed")
	state.handleSettingsEditing(tcell.NewEventKey(tcell.KeyEscape, 0, 0))
	if state.Config.Secret != "" {
		t.Fatalf("escape must restore the original value, got %q", state.Config.Secret)
	}
}

func TestClashProbeDerivesTransferRate(t *testing.T) {
	controller := newFakeController(t)
	probe, err := newClashProbe(controller.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	// The first poll has no predecessor, so the rate stays zero.
	info, err := probe.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Uplink != 0 || info.Downlink != 0 {
		t.Fatalf("the first poll must not report a rate: %+v", info)
	}
	if info.UpTotal != 4 || info.DownTotal != 5 {
		t.Fatalf("unexpected totals: %+v", info)
	}
	if info.Conns != 1 {
		t.Fatalf("unexpected connection count: %+v", info)
	}

	// The second poll derives the rate from the difference between the
	// two samples.
	probe.lastTime = time.Now().Add(-2 * time.Second)
	info, err = probe.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Uplink != 0 || info.Downlink != 0 {
		t.Fatalf("the totals did not change, so the rate must stay zero: %+v", info)
	}
}

func TestServerMonitorRecordsErrors(t *testing.T) {
	state := newTestApp()
	state.Servers = []ServerMonitor{{Entry: config.ServerEntry{Name: "a"}}}
	state.handleServer(Event{Kind: EventServer, ServerIndex: 0, Err: errBoom{}})
	if state.Servers[0].Connected {
		t.Fatal("the monitor must be offline after an error")
	}
	if state.Servers[0].Err == "" {
		t.Fatal("the error must be visible")
	}
	state.handleServer(Event{Kind: EventServer, ServerIndex: 99})
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
