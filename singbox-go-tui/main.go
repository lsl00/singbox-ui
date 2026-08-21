package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
)

func main() {
	cli, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "use --help for usage")
		return
	}
	if cli.ShowHelp {
		printHelp()
		return
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create terminal:", err)
		return
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to initialize terminal:", err)
		return
	}
	terminalQuit := make(chan struct{})
	defer func() {
		close(terminalQuit)
		screen.Fini()
	}()
	screen.HideCursor()

	app := newApp(cli.API)
	networkEvents := make(chan NetworkEvent, 128)
	terminalEvents := make(chan tcell.Event, 32)
	go screen.ChannelEvents(terminalEvents, terminalQuit)

	app.loadServers(cli.ServersPath)
	app.pollServers(networkEvents)

	if cli.AutoConnect {
		app.connect(networkEvents)
	}

	drawTicker := time.NewTicker(100 * time.Millisecond)
	defer drawTicker.Stop()
	statusTicker := time.NewTicker(500 * time.Millisecond)
	defer statusTicker.Stop()
	groupsTicker := time.NewTicker(3 * time.Second)
	defer groupsTicker.Stop()
	connectionsTicker := time.NewTicker(2500 * time.Millisecond)
	defer connectionsTicker.Stop()
	logsTicker := time.NewTicker(2 * time.Second)
	defer logsTicker.Stop()
	metadataTicker := time.NewTicker(15 * time.Second)
	defer metadataTicker.Stop()
	serversTicker := time.NewTicker(2 * time.Second)
	defer serversTicker.Stop()

	drawScreen(screen, app)
	for !app.shouldQuit {
		select {
		case <-drawTicker.C:
			drawScreen(screen, app)
		case <-statusTicker.C:
			app.pollStatus(networkEvents)
		case <-groupsTicker.C:
			app.pollGroups(networkEvents)
		case <-connectionsTicker.C:
			app.pollConnections(networkEvents)
		case <-logsTicker.C:
			app.pollLogs(networkEvents)
		case <-metadataTicker.C:
			app.pollMetadata(networkEvents)
		case <-serversTicker.C:
			app.pollServers(networkEvents)
		case event, ok := <-terminalEvents:
			if !ok {
				app.shouldQuit = true
				continue
			}
			switch value := event.(type) {
			case *tcell.EventKey:
				app.handleKey(value, networkEvents)
			case *tcell.EventResize:
				screen.Sync()
			case *tcell.EventError:
				app.error = value.Error()
			}
		case event := <-networkEvents:
			app.handleNetwork(event, networkEvents)
		}
	}
}
