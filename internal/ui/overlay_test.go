package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
)

// TestTheOverlayDrawsTheDetailPanel checks that the detail panel of the
// selected connection shows up in the overlay of an opened instance.
func TestTheOverlayDrawsTheDetailPanel(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(100, 30)

	state := newTestApp()
	state.View = app.ViewServers
	state.OpenedServer = 0

	Draw(screen, state)
	if screenContains(screen, "Connection details") {
		t.Fatalf("the detail panel is shown before it is asked for")
	}

	state.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	Draw(screen, state)
	if !screenContains(screen, "Connection details") {
		t.Fatalf("the detail panel is missing")
	}
}

// screenContains reports whether a line of the screen holds the text.
func screenContains(screen tcell.SimulationScreen, needle string) bool {
	cells, width, height := screen.GetContents()
	for y := 0; y < height; y++ {
		line := ""
		for x := 0; x < width; x++ {
			runes := cells[y*width+x].Runes
			if len(runes) == 0 {
				line += " "
				continue
			}
			line += string(runes)
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}
