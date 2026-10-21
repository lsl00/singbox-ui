package ui

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
)

func newTestApp() *app.App {
	state := app.New(config.APIConfig{URL: config.DefaultAPIURL})
	state.Connected = true
	state.Version = &clash.Version{Version: "1.19.27"}
	state.Runtime = &clash.Runtime{Mode: "rule", LogLevel: "info"}
	state.Traffic = clash.Traffic{Up: 1024, Down: 2048, UpTotal: 1 << 20, DownTotal: 1 << 21}
	state.TrafficUp = true
	state.Memory = 12 << 20
	state.MemoryUp = true
	state.HistoryUp = []int64{100, 200, 300}
	state.HistoryDown = []int64{400, 500, 600}
	state.Proxies = map[string]clash.Proxy{
		"Selector": {
			Name: "Selector", Type: "Selector", All: []string{"Node A", "Node B"},
			Now:     "Node A",
			History: []clash.DelayHistory{{Delay: 120}},
		},
		"Node A": {
			Name: "Node A", Type: "Shadowsocks",
			History: []clash.DelayHistory{{Delay: 120}},
		},
		"Node B": {Name: "Node B", Type: "VMess"},
	}
	state.Connections = []app.ConnInfo{
		{
			ID:          "conn-1",
			Source:      "10.0.0.1:1234",
			Destination: "example.com",
			Protocol:    "HTTP",
			Network:     "tcp",
			Upload:      10,
			Download:    20,
			CreatedAt:   time.Now().Add(-30 * time.Second),
			Chain:       []string{"Selector", "Node A"},
			Rule:        "DomainSuffix",
		},
	}
	state.Logs = []app.LogLine{
		{Time: time.Now(), Level: "info", Text: "kernel started"},
		{Time: time.Now(), Level: "error", Text: "something failed"},
	}
	return state
}

func TestDrawAllViewsAtSeveralSizes(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{10, 6},
		{40, 12},
		{80, 24},
		{160, 50},
	} {
		for view := app.ViewOverview; view < app.ViewCount; view++ {
			screen := tcell.NewSimulationScreen("UTF-8")
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			screen.SetSize(size.width, size.height)
			state := newTestApp()
			state.View = view
			state.ShowDetails = true
			state.Help = true
			Draw(screen, state)
			state.Help = false
			Draw(screen, state)
			screen.Fini()
		}
	}
}

func TestDrawWithoutServerConfiguration(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(80, 24)
	state := newTestApp()
	state.View = app.ViewServers
	Draw(screen, state)
	screen.Fini()
}

func TestLogStartFollowsTheTail(t *testing.T) {
	if got := logStart(10, 5, true, 0); got != 5 {
		t.Fatalf("expected the last page, got %d", got)
	}
	if got := logStart(10, 5, false, 2); got != 3 {
		t.Fatalf("expected the page above the tail, got %d", got)
	}
	if got := logStart(3, 5, true, 0); got != 0 {
		t.Fatalf("expected the first page, got %d", got)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := formatBytes(0); got != "0 B" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatBytes(1536); got != "1.50 KB" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatRate(1024); got != "1.00 KB/s" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatAge(0); got != "0s" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatAge(90 * 1000); got != "1m" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatAge(3*3600*1000 + 25*60*1000); got != "3h25m" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatDelay(0, true); got != "timeout" {
		t.Fatalf("unexpected formatting: %q", got)
	}
	if got := formatDelay(120, true); got != "120ms" {
		t.Fatalf("unexpected formatting: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 4); got != "a..." {
		t.Fatalf("unexpected truncation: %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Fatalf("unexpected truncation: %q", got)
	}
	if got := truncate("abc", 0); got != "" {
		t.Fatalf("unexpected truncation: %q", got)
	}
}
