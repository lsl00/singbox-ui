package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
)

// fakeController serves the subset of the mihomo API the dashboard uses.
type fakeController struct {
	server *httptest.Server
}

func newFakeController(t *testing.T) *fakeController {
	t.Helper()
	controller := &fakeController{}
	upgrader := websocket.Upgrader{}

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, map[string]any{"meta": true, "version": "1.19.27"})
	})
	mux.HandleFunc("/configs", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, map[string]any{"mode": "rule", "log-level": "info", "mixed-port": 7890})
	})
	mux.HandleFunc("/proxies", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, map[string]any{"proxies": map[string]any{
			"Selector": map[string]any{
				"name": "Selector", "type": "Selector", "now": "Node A",
				"all": []string{"Node A"},
			},
			"Node A": map[string]any{"name": "Node A", "type": "Shadowsocks"},
		}})
	})
	mux.HandleFunc("/traffic", func(writer http.ResponseWriter, request *http.Request) {
		pushStream(writer, request, upgrader, map[string]any{
			"up": 10, "down": 20, "upTotal": 30, "downTotal": 40,
		})
	})
	mux.HandleFunc("/memory", func(writer http.ResponseWriter, request *http.Request) {
		pushStream(writer, request, upgrader, map[string]any{"inuse": 4096})
	})
	mux.HandleFunc("/connections", func(writer http.ResponseWriter, request *http.Request) {
		pushStream(writer, request, upgrader, map[string]any{
			"downloadTotal": 5, "uploadTotal": 4, "memory": 128,
			"connections": []map[string]any{
				{"id": "c1", "metadata": map[string]any{"host": "example.com"}, "upload": 1, "download": 2, "start": "2024-01-01T00:00:00Z"},
			},
		})
	})
	mux.HandleFunc("/logs", func(writer http.ResponseWriter, request *http.Request) {
		pushStream(writer, request, upgrader, map[string]any{"type": "warning", "payload": "careful"})
	})

	controller.server = httptest.NewServer(mux)
	t.Cleanup(controller.server.Close)
	return controller
}

func writeJSON(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

func pushStream(writer http.ResponseWriter, request *http.Request, upgrader websocket.Upgrader, payload any) {
	if request.Header.Get("Upgrade") != "websocket" {
		writeJSON(writer, payload)
		return
	}
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for i := 0; i < 20; i++ {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pump applies the events of the dashboard until the condition holds.
func pump(t *testing.T, state *App, timeout time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case event := <-state.Events():
			state.HandleEvent(event)
			if done() {
				return
			}
		case <-time.After(20 * time.Millisecond):
			if done() {
				return
			}
		}
	}
	if !done() {
		t.Fatal("the dashboard did not reach the expected state in time")
	}
}

func TestDashboardCollectsTheControllerState(t *testing.T) {
	controller := newFakeController(t)
	state := New(config.APIConfig{URL: controller.server.URL})
	state.Connect()

	pump(t, state, 10*time.Second, func() bool { return state.Connected })
	if state.Version == nil || state.Version.Version != "1.19.27" {
		t.Fatalf("unexpected version: %+v", state.Version)
	}

	pump(t, state, 10*time.Second, func() bool {
		return state.TrafficUp && state.MemoryUp && len(state.Connections) > 0 && len(state.Logs) > 0 &&
			state.Runtime != nil && len(state.GroupNames) > 0
	})

	if state.Traffic.Down != 20 || state.Traffic.DownTotal != 40 {
		t.Fatalf("unexpected traffic: %+v", state.Traffic)
	}
	if state.Memory != 4096 {
		t.Fatalf("unexpected memory: %d", state.Memory)
	}
	if len(state.Connections) != 1 || state.Connections[0].ID != "c1" {
		t.Fatalf("unexpected connections: %+v", state.Connections)
	}
	if len(state.Logs) != 1 || state.Logs[0].Level != "warning" {
		t.Fatalf("unexpected logs: %+v", state.Logs)
	}
	if state.Runtime == nil || state.Runtime.Mode != "rule" {
		t.Fatalf("unexpected runtime config: %+v", state.Runtime)
	}
	if len(state.GroupNames) != 1 || state.GroupNames[0] != "Selector" {
		t.Fatalf("unexpected groups: %+v", state.GroupNames)
	}
	if !state.StreamUp["traffic"] || !state.StreamUp["logs"] {
		t.Fatalf("unexpected stream states: %+v", state.StreamUp)
	}

	state.Disconnect()
	if state.Connected || len(state.Connections) != 0 {
		t.Fatalf("disconnect must reset the state")
	}
}

func TestDisconnectStopsTheStreams(t *testing.T) {
	controller := newFakeController(t)
	state := New(config.APIConfig{URL: controller.server.URL})
	state.Connect()
	pump(t, state, 10*time.Second, func() bool { return state.TrafficUp })

	state.Disconnect()
	time.Sleep(200 * time.Millisecond)
	select {
	case event := <-state.Events():
		// Events of the cancelled generation are dropped by the gate.
		if event.Gen == state.gen {
			t.Fatalf("unexpected event after the disconnect: %+v", event)
		}
	default:
	}
	if state.TrafficUp {
		t.Fatalf("the traffic stream must be marked as down")
	}
}

func TestProxyDelayRejectsUnknownProxy(t *testing.T) {
	client, err := clash.New(clash.Config{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ProxyDelay(context.Background(), "missing", clash.DefaultTestURL, clash.DefaultDelayTimeout); err == nil {
		t.Fatal("expected an error for an unreachable controller")
	}
}
