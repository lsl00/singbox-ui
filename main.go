// Command mihomo-tui is a terminal dashboard for the mihomo kernel.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"

	"mihomo-tui/internal/app"
	"mihomo-tui/internal/config"
	"mihomo-tui/internal/ui"
)

func main() {
	cli, err := config.ParseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "use --help for usage")
		return
	}
	if cli.ShowHelp {
		config.PrintHelp()
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

	dashboard := app.New(cli.API)
	dashboard.SetServers(config.LoadServerEntries(cli.ServersPath))
	defer dashboard.StopServers()

	terminalEvents := make(chan tcell.Event, 32)
	go screen.ChannelEvents(terminalEvents, terminalQuit)

	if cli.AutoConnect {
		dashboard.Connect()
	}
	dashboard.RefreshServers()

	drawTicker := time.NewTicker(100 * time.Millisecond)
	defer drawTicker.Stop()
	proxyTicker := time.NewTicker(3 * time.Second)
	defer proxyTicker.Stop()
	serverTicker := time.NewTicker(2 * time.Second)
	defer serverTicker.Stop()
	metaTicker := time.NewTicker(15 * time.Second)
	defer metaTicker.Stop()

	for !dashboard.ShouldQuit {
		select {
		case <-drawTicker.C:
			ui.Draw(screen, dashboard)
		case <-proxyTicker.C:
			dashboard.RefreshProxies()
		case <-serverTicker.C:
			dashboard.RefreshServers()
		case <-metaTicker.C:
			dashboard.RefreshRuntime()
		case event := <-dashboard.Events():
			dashboard.HandleEvent(event)
		case event, ok := <-terminalEvents:
			if !ok {
				dashboard.ShouldQuit = true
				continue
			}
			switch value := event.(type) {
			case *tcell.EventKey:
				dashboard.HandleKey(value)
			case *tcell.EventResize:
				screen.Sync()
			case *tcell.EventError:
				dashboard.Error = value.Error()
			}
		}
	}
}
