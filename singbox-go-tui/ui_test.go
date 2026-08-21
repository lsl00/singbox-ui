package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestDrawScreenHandlesSmallTerminals(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{10, 6},
		{40, 12},
		{80, 24},
	} {
		screen := tcell.NewSimulationScreen("UTF-8")
		if err := screen.Init(); err != nil {
			t.Fatal(err)
		}
		screen.SetSize(size.width, size.height)
		app := newApp(APIConfig{URL: defaultAPIURL})
		app.help = true
		drawScreen(screen, app)
		app.help = false
		app.view = ViewServers
		status := StatusInfo{Uplink: 1024, Downlink: 2048, UplinkTotal: 1 << 20, DownlinkTotal: 1 << 21}
		client, err := newAPIClient(APIConfig{URL: defaultAPIURL})
		if err != nil {
			t.Fatal(err)
		}
		app.servers = []ServerMonitor{
			{Entry: ServerEntry{Name: "alpha", BaseURL: defaultAPIURL}, Client: client, Connected: true, Status: &status},
			{Entry: ServerEntry{Name: "beta", BaseURL: defaultAPIURL}, Err: "connection refused"},
		}
		drawScreen(screen, app)
		screen.Fini()
	}
}
