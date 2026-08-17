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
		screen.Fini()
	}
}
