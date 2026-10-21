package clash

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeController is a minimal stand-in for the mihomo external controller.
type fakeController struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	mode     string
	selected map[string]string
	requests []string
}

func newFakeController(t *testing.T) *fakeController {
	t.Helper()
	controller := &fakeController{t: t, mode: "rule", selected: map[string]string{}}
	upgrader := websocket.Upgrader{}

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		writeJSON(writer, map[string]any{"meta": true, "version": "1.19.27"})
	})
	mux.HandleFunc("/configs", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		switch request.Method {
		case http.MethodGet:
			writeJSON(writer, map[string]any{
				"mode":       controller.currentMode(),
				"log-level":  "info",
				"mixed-port": 7890,
			})
		case http.MethodPatch:
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			controller.mu.Lock()
			controller.mode = body["mode"]
			controller.mu.Unlock()
			writer.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/restart", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		writeJSON(writer, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/proxies", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		writeJSON(writer, map[string]any{"proxies": controller.proxyMap()})
	})
	mux.HandleFunc("/proxies/", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		name := strings.TrimPrefix(request.URL.Path, "/proxies/")
		switch {
		case strings.HasSuffix(name, "/delay"):
			writeJSON(writer, map[string]any{"delay": 120})
		case request.Method == http.MethodPut:
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			controller.mu.Lock()
			controller.selected[name] = body["name"]
			controller.mu.Unlock()
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(writer, controller.proxyMap()[name])
		}
	})
	mux.HandleFunc("/group/", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		writeJSON(writer, map[string]uint16{"Node A": 100, "Node B": 200})
	})
	mux.HandleFunc("/connections", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		if request.Method == http.MethodDelete {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(writer, controller.snapshot())
	})
	mux.HandleFunc("/connections/", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/traffic", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		controller.stream(writer, request, upgrader, map[string]any{
			"up": 100, "down": 200, "upTotal": 300, "downTotal": 400,
		})
	})
	mux.HandleFunc("/memory", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		controller.stream(writer, request, upgrader, map[string]any{"inuse": 2048, "oslimit": 0})
	})
	mux.HandleFunc("/logs", func(writer http.ResponseWriter, request *http.Request) {
		controller.record(request)
		controller.stream(writer, request, upgrader, map[string]any{"type": "info", "payload": "hello"})
	})

	controller.server = httptest.NewServer(mux)
	t.Cleanup(controller.server.Close)
	return controller
}

func (c *fakeController) record(request *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request.Method+" "+request.URL.Path)
}

func (c *fakeController) currentMode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode
}

func (c *fakeController) selection(group string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selected[group]
}

func (c *fakeController) proxyMap() map[string]any {
	return map[string]any{
		"Selector": map[string]any{
			"name":  "Selector",
			"type":  "Selector",
			"now":   "Node A",
			"all":   []string{"Node A", "Node B"},
			"alive": true,
			"history": []map[string]any{
				{"time": "2024-01-01T00:00:00Z", "delay": 120},
			},
		},
		"Node A": map[string]any{
			"name":  "Node A",
			"type":  "Shadowsocks",
			"alive": true,
			"history": []map[string]any{
				{"time": "2024-01-01T00:00:00Z", "delay": 120},
			},
		},
		"Node B": map[string]any{
			"name":  "Node B",
			"type":  "VMess",
			"alive": false,
		},
	}
}

func (c *fakeController) snapshot() map[string]any {
	return map[string]any{
		"downloadTotal": 4096,
		"uploadTotal":   2048,
		"memory":        1024,
		"connections": []map[string]any{
			{
				"id":       "conn-1",
				"metadata": map[string]any{"network": "tcp", "type": "HTTP", "host": "example.com", "sourceIP": "10.0.0.1", "sourcePort": "1234", "destinationIP": "1.2.3.4", "destinationPort": "443"},
				"upload":   10,
				"download": 20,
				"start":    "2024-01-01T00:00:00Z",
				"chains":   []string{"Selector", "Node A"},
				"rule":     "DomainSuffix",
			},
		},
	}
}

func (c *fakeController) stream(writer http.ResponseWriter, request *http.Request, upgrader websocket.Upgrader, payload any) {
	if request.Header.Get("Upgrade") != "websocket" {
		writeJSON(writer, payload)
		return
	}
	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	for i := 0; i < 3; i++ {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeJSON(writer http.ResponseWriter, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(payload)
}

func TestClientVersionAndAuth(t *testing.T) {
	controller := newFakeController(t)
	client, err := New(Config{URL: controller.server.URL, Secret: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != "1.19.27" {
		t.Fatalf("unexpected version: %+v", version)
	}
}

func TestClientRejectsInvalidURL(t *testing.T) {
	for _, raw := range []string{"", "localhost:9090", "ftp://localhost:9090", "://x"} {
		if _, err := New(Config{URL: raw}); err == nil {
			t.Fatalf("expected an error for %q", raw)
		}
	}
}

func TestClientProxiesDistinguishGroups(t *testing.T) {
	controller := newFakeController(t)
	client, err := New(Config{URL: controller.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := client.Proxies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	group := proxies["Selector"]
	if !group.IsGroup() || !group.IsSelector() {
		t.Fatalf("Selector must be a selector group: %+v", group)
	}
	if len(group.All) != 2 || group.Now != "Node A" {
		t.Fatalf("unexpected group members: %+v", group)
	}
	node := proxies["Node A"]
	if node.IsGroup() {
		t.Fatalf("Node A must not be a group: %+v", node)
	}
	delay, known := node.LastDelay()
	if !known || delay != 120 {
		t.Fatalf("unexpected delay: %d %v", delay, known)
	}
	if _, known := proxies["Node B"].LastDelay(); known {
		t.Fatalf("Node B has no history")
	}
}

func TestClientSelectProxyEscapesName(t *testing.T) {
	controller := newFakeController(t)
	client, err := New(Config{URL: controller.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SelectProxy(context.Background(), "Proxy/日本", "Node A"); err != nil {
		t.Fatal(err)
	}
	if got := controller.selection("Proxy/日本"); got != "Node A" {
		t.Fatalf("unexpected selection: %q", got)
	}
}

func TestClientSetModeAndRuntime(t *testing.T) {
	controller := newFakeController(t)
	client, err := New(Config{URL: controller.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetMode(context.Background(), "global"); err != nil {
		t.Fatal(err)
	}
	if controller.currentMode() != "global" {
		t.Fatalf("mode was not applied: %q", controller.currentMode())
	}
	runtime, err := client.Runtime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Mode != "global" {
		t.Fatalf("unexpected runtime config: %+v", runtime)
	}
}

func TestClientDelayAndConnections(t *testing.T) {
	controller := newFakeController(t)
	client, err := New(Config{URL: controller.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	delay, err := client.ProxyDelay(context.Background(), "Node A", DefaultTestURL, DefaultDelayTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if delay != 120 {
		t.Fatalf("unexpected delay: %d", delay)
	}
	delays, err := client.GroupDelay(context.Background(), "Selector", DefaultTestURL, DefaultDelayTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if delays["Node B"] != 200 {
		t.Fatalf("unexpected group delays: %+v", delays)
	}
	snapshot, err := client.Connections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.DownloadTotal != 4096 || len(snapshot.Connections) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Connections[0].Destination() != "example.com" {
		t.Fatalf("unexpected destination: %q", snapshot.Connections[0].Destination())
	}
}

func TestClientStreams(t *testing.T) {
	controller := newFakeController(t)
	client, err := New(Config{URL: controller.server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	trafficStream, err := client.DialStream(ctx, "/traffic", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer trafficStream.Close()
	var traffic Traffic
	if err := trafficStream.ReadJSON(ctx, &traffic); err != nil {
		t.Fatal(err)
	}
	if traffic.Up != 100 || traffic.DownTotal != 400 {
		t.Fatalf("unexpected traffic: %+v", traffic)
	}

	memoryStream, err := client.DialStream(ctx, "/memory", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer memoryStream.Close()
	var memory Memory
	if err := memoryStream.ReadJSON(ctx, &memory); err != nil {
		t.Fatal(err)
	}
	if memory.Inuse != 2048 {
		t.Fatalf("unexpected memory: %+v", memory)
	}

	logStream, err := client.DialStream(ctx, "/logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer logStream.Close()
	var entry LogEntry
	if err := logStream.ReadJSON(ctx, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Type != "info" || entry.Payload != "hello" {
		t.Fatalf("unexpected log entry: %+v", entry)
	}
}

func TestClientStreamURLScheme(t *testing.T) {
	client, err := New(Config{URL: "http://127.0.0.1:9090"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := client.websocketURL("/traffic", nil)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "ws://127.0.0.1:9090/traffic" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
	secure, err := New(Config{URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = secure.websocketURL("/logs", map[string][]string{"level": {"debug"}})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "wss://example.com/logs?level=debug" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}
}

func TestClientReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer server.Close()
	client, err := New(Config{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Version(context.Background()); err == nil {
		t.Fatalf("expected an error for HTTP 401")
	}
}
