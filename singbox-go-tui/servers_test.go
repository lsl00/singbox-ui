package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadServerEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers")
	content := "# comment line\n" +
		"\n" +
		"main 127.0.0.1:9000\n" +
		"backup example.com:9443\n" +
		"custom http://1.2.3.4:8080\n" +
		"broken\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	entries := loadServerEntries(path)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "main" || entries[0].BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("unexpected entry 0: %+v", entries[0])
	}
	if entries[1].Name != "backup" || entries[1].BaseURL != "http://example.com:9443" {
		t.Fatalf("unexpected entry 1: %+v", entries[1])
	}
	if entries[2].BaseURL != "http://1.2.3.4:8080" {
		t.Fatalf("explicit scheme must be preserved: %+v", entries[2])
	}
}

func TestLoadServerEntriesMissingFile(t *testing.T) {
	entries := loadServerEntries(filepath.Join(t.TempDir(), "missing"))
	if len(entries) != 0 {
		t.Fatalf("expected no entries for missing file, got %+v", entries)
	}
}

func TestLoadServerEntriesEmptyPath(t *testing.T) {
	if entries := loadServerEntries(""); entries != nil {
		t.Fatalf("expected nil entries for empty path, got %+v", entries)
	}
}

func TestParseCLIServersFlag(t *testing.T) {
	cli, err := parseCLI([]string{"--servers", "/tmp/custom-servers"})
	if err != nil {
		t.Fatal(err)
	}
	if cli.ServersPath != "/tmp/custom-servers" {
		t.Fatalf("unexpected servers path: %q", cli.ServersPath)
	}

	defaults, err := parseCLI(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.ServersPath != "" {
		t.Fatalf("servers path must default to empty, got %q", defaults.ServersPath)
	}
}

func TestPollServersAndEventHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := appendInt64(nil, 2, 3)
		frame, err := encodeFrame(body)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = writer.Write(frame)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	app := newApp(APIConfig{URL: defaultAPIURL})
	client, err := newAPIClient(APIConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	app.servers = []ServerMonitor{
		{Entry: ServerEntry{Name: "a", BaseURL: server.URL}, Client: client},
	}

	events := make(chan NetworkEvent, 8)
	app.pollServers(events)
	select {
	case event := <-events:
		if event.Kind != NetworkServerStatus || event.ServerIndex != 0 {
			t.Fatalf("unexpected event: %+v", event)
		}
		if event.Err != nil {
			t.Fatalf("unexpected error: %v", event.Err)
		}
		if event.Status.Goroutines != 3 {
			t.Fatalf("unexpected status: %+v", event.Status)
		}
		app.handleServerStatus(event)
		if !app.servers[0].Connected || app.servers[0].Status == nil || app.servers[0].Busy {
			t.Fatalf("monitor should be connected with status: %+v", app.servers[0])
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no event received")
	}

	failure := NetworkEvent{Kind: NetworkServerStatus, ServerIndex: 0, Err: errors.New("boom")}
	app.handleServerStatus(failure)
	if app.servers[0].Connected || app.servers[0].Err != "boom" {
		t.Fatalf("monitor should be offline with error: %+v", app.servers[0])
	}

	outOfRange := NetworkEvent{Kind: NetworkServerStatus, ServerIndex: 99}
	app.handleServerStatus(outOfRange)
}
