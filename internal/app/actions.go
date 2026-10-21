package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"

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

// testSelectedNode measures the latency of the node under the cursor.
func (a *App) testSelectedNode() {
	_, node, ok := a.selectedNode()
	if !ok {
		return
	}
	a.runAction(ActionProxyDelay, "testing "+node, func(ctx context.Context, client *clash.Client) (Event, error) {
		delay, err := client.ProxyDelay(ctx, node, clash.DefaultTestURL, clash.DefaultDelayTimeout)
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
	a.runAction(ActionGroupDelay, "testing group "+groupName, func(ctx context.Context, client *clash.Client) (Event, error) {
		delays, err := client.GroupDelay(ctx, groupName, clash.DefaultTestURL, clash.DefaultDelayTimeout)
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

// handleSettingsKey processes a key press on the settings page.
func (a *App) handleSettingsKey(event *tcell.EventKey) {
	switch event.Key() {
	case tcell.KeyUp:
		a.SettingsField = maxInt(a.SettingsField-1, 0)
	case tcell.KeyDown, tcell.KeyTab:
		a.SettingsField = minInt(a.SettingsField+1, settingsFieldCount-1)
	case tcell.KeyBacktab:
		a.SettingsField = maxInt(a.SettingsField-1, 0)
	case tcell.KeyEnter:
		a.beginSettingsEdit()
	case tcell.KeyRune:
		switch event.Rune() {
		case 'e':
			a.beginSettingsEdit()
		case 'c':
			// Reconnecting discards the state of the old controller, so
			// an established connection is closed first.
			if a.Connected || a.Connecting {
				a.Disconnect()
			}
			a.Connect()
		case 'd':
			a.Disconnect()
		case 'R':
			a.reloadConfig()
		case 'X':
			a.restartKernel()
		case 'F':
			a.flushDNSCache()
		case 'P':
			a.flushFakeIPCache()
		}
	}
}

// settingsFieldCount is the number of editable settings fields.
const settingsFieldCount = 3

// beginSettingsEdit starts editing the selected field.
func (a *App) beginSettingsEdit() {
	a.SettingsEditing = true
	a.SettingsOriginal = a.settingsValue()
	a.SettingsCursor = len([]rune(a.settingsValue()))
	a.Message = "editing; Enter accepts, Esc cancels editing"
}

// handleSettingsEditing processes a key press while a field is edited.
func (a *App) handleSettingsEditing(event *tcell.EventKey) {
	value := []rune(a.settingsValue())
	switch event.Key() {
	case tcell.KeyEscape:
		a.setSettingsValue(a.SettingsOriginal)
		a.SettingsEditing = false
	case tcell.KeyEnter:
		a.SettingsEditing = false
		a.Message = "field updated; press c to connect"
	case tcell.KeyTab:
		a.SettingsEditing = false
		a.SettingsField = minInt(a.SettingsField+1, settingsFieldCount-1)
		a.beginSettingsEdit()
	case tcell.KeyLeft:
		a.SettingsCursor = maxInt(a.SettingsCursor-1, 0)
	case tcell.KeyRight:
		a.SettingsCursor = minInt(a.SettingsCursor+1, len(value))
	case tcell.KeyHome:
		a.SettingsCursor = 0
	case tcell.KeyEnd:
		a.SettingsCursor = len(value)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.SettingsCursor > 0 {
			value = append(value[:a.SettingsCursor-1], value[a.SettingsCursor:]...)
			a.SettingsCursor--
			a.setSettingsValue(string(value))
		}
	case tcell.KeyDelete:
		if a.SettingsCursor < len(value) {
			value = append(value[:a.SettingsCursor], value[a.SettingsCursor+1:]...)
			a.setSettingsValue(string(value))
		}
	case tcell.KeyRune:
		if event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			position := minInt(a.SettingsCursor, len(value))
			value = append(value, 0)
			copy(value[position+1:], value[position:])
			value[position] = event.Rune()
			a.SettingsCursor = position + 1
			a.setSettingsValue(string(value))
		}
	}
}

func (a *App) settingsValue() string {
	switch a.SettingsField {
	case 0:
		return a.Config.URL
	case 1:
		return a.Config.Secret
	default:
		return a.ConfigPath
	}
}

func (a *App) setSettingsValue(value string) {
	switch a.SettingsField {
	case 0:
		a.Config.URL = value
	case 1:
		a.Config.Secret = value
	default:
		a.ConfigPath = value
	}
}
