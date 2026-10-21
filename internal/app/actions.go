package app

import (
	"context"
	"fmt"
	"time"

	"mihomo-tui/internal/clash"
)

// tunnelModes are the runtime modes mihomo understands.
var tunnelModes = []string{"rule", "global", "direct"}

// runAction performs a write operation in the background.
func (a *App) runAction(kind ActionKind, message string, run func(context.Context, *clash.Client) (Event, error)) {
	if a.client == nil {
		a.Message = "not connected"
		return
	}
	if a.busy[EventAction] {
		a.Message = "previous action still in progress"
		return
	}
	a.busy[EventAction] = true
	if message != "" {
		a.Message = message
	}
	client := a.client
	gen := a.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		event, err := run(ctx, client)
		event.Kind = EventAction
		event.Action = kind
		event.Gen = gen
		event.Err = err
		a.events <- event
	}()
}

// selectedNode returns the group and node under the cursor.
func (a *App) selectedNode() (string, string, bool) {
	row, found := a.SelectedGroupRow()
	if !found || !row.IsItem || row.GroupIndex >= len(a.GroupNames) {
		return "", "", false
	}
	groupName := a.GroupNames[row.GroupIndex]
	group := a.Proxies[groupName]
	if row.ItemIndex >= len(group.All) {
		return "", "", false
	}
	return groupName, group.All[row.ItemIndex], true
}

// selectHighlightedNode switches the active node of a selector group.
func (a *App) selectHighlightedNode() {
	groupName, node, ok := a.selectedNode()
	if !ok {
		return
	}
	group := a.Proxies[groupName]
	if !group.IsSelector() {
		a.Message = fmt.Sprintf("%s is a %s group and cannot be switched manually", groupName, group.Type)
		return
	}
	a.runAction(ActionSelectProxy, "selecting "+node, func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{Group: groupName, Node: node}, client.SelectProxy(ctx, groupName, node)
	})
}

// testURLFor returns the health check URL of a group.
//
// mihomo configures a test URL per group. Using it keeps the latency
// history of the nodes consistent with the health checks of the group
// itself. Groups without a configured URL fall back to the default.
func (a *App) testURLFor(groupName string) string {
	if group, ok := a.Proxies[groupName]; ok && group.TestURL != "" {
		return group.TestURL
	}
	return clash.DefaultTestURL
}

// testSelectedNode measures the latency of the node under the cursor.
func (a *App) testSelectedNode() {
	groupName, node, ok := a.selectedNode()
	if !ok {
		return
	}
	testURL := a.testURLFor(groupName)
	a.runAction(ActionProxyDelay, "testing "+node, func(ctx context.Context, client *clash.Client) (Event, error) {
		delay, err := client.ProxyDelay(ctx, node, testURL, clash.DefaultDelayTimeout)
		return Event{Node: node, Delay: delay}, err
	})
}

// testSelectedGroup measures the latency of every node of a group.
func (a *App) testSelectedGroup() {
	row, found := a.SelectedGroupRow()
	if !found || row.GroupIndex >= len(a.GroupNames) {
		return
	}
	groupName := a.GroupNames[row.GroupIndex]
	testURL := a.testURLFor(groupName)
	a.runAction(ActionGroupDelay, "testing group "+groupName, func(ctx context.Context, client *clash.Client) (Event, error) {
		delays, err := client.GroupDelay(ctx, groupName, testURL, clash.DefaultDelayTimeout)
		return Event{Group: groupName, Delays: delays}, err
	})
}

// toggleSelectedGroup expands or collapses a group in the tree.
func (a *App) toggleSelectedGroup() {
	row, found := a.SelectedGroupRow()
	if !found || row.GroupIndex >= len(a.GroupNames) {
		return
	}
	a.ToggleGroup(a.GroupNames[row.GroupIndex])
}

// cycleMode switches the runtime mode of the kernel.
func (a *App) cycleMode(direction int) {
	current := a.Mode()
	index := 0
	for i, mode := range tunnelModes {
		if mode == current {
			index = i
			break
		}
	}
	next := (index + direction) % len(tunnelModes)
	if next < 0 {
		next += len(tunnelModes)
	}
	mode := tunnelModes[next]
	a.runAction(ActionSetMode, "setting mode to "+mode, func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{Mode: mode}, client.SetMode(ctx, mode)
	})
}

// closeSelectedConnection closes the connection under the cursor.
func (a *App) closeSelectedConnection() {
	connection, ok := a.SelectedConnection()
	if !ok {
		return
	}
	a.runAction(ActionCloseConnection, "closing connection", func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{}, client.CloseConnection(ctx, connection.ID)
	})
}

// closeAllConnections closes every tracked connection.
func (a *App) closeAllConnections() {
	a.runAction(ActionCloseAll, "closing all connections", func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{}, client.CloseAllConnections(ctx)
	})
}

// reloadConfig asks the kernel to reload its configuration file.
func (a *App) reloadConfig() {
	path := a.ConfigPath
	a.runAction(ActionReloadConfig, "reloading configuration", func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{}, client.ReloadConfig(ctx, path)
	})
}

// restartKernel asks the kernel to restart itself.
func (a *App) restartKernel() {
	a.runAction(ActionRestart, "restarting kernel", func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{}, client.Restart(ctx)
	})
}

// flushDNSCache clears the resolver cache of the kernel.
func (a *App) flushDNSCache() {
	a.runAction(ActionFlushDNS, "flushing dns cache", func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{}, client.FlushDNSCache(ctx)
	})
}

// flushFakeIPCache clears the fake-ip cache of the kernel.
func (a *App) flushFakeIPCache() {
	a.runAction(ActionFlushFakeIP, "flushing fake-ip cache", func(ctx context.Context, client *clash.Client) (Event, error) {
		return Event{}, client.FlushFakeIPCache(ctx)
	})
}
