package app

import "sort"

// rebuildGroups recomputes the sorted group list and the flattened tree.
func (a *App) rebuildGroups() {
	names := make([]string, 0)
	for name, proxy := range a.Proxies {
		if proxy.IsGroup() && !proxy.Hidden {
			names = append(names, name)
		}
	}
	sortGroups(names)
	a.GroupNames = names
	a.invalidateGroups()
	a.clampGroupCursor()
}

// globalGroup is the group mihomo creates for the global mode. It holds
// every proxy of the configuration and is therefore not very useful in
// the tree, so it is listed last.
const globalGroup = "GLOBAL"

// sortGroups orders the groups by name and keeps the GLOBAL group last.
func sortGroups(names []string) {
	sort.SliceStable(names, func(i, j int) bool {
		left, right := names[i], names[j]
		if left == globalGroup {
			return false
		}
		if right == globalGroup {
			return true
		}
		return left < right
	})
}

// invalidateGroups drops the cached tree so it is rebuilt on demand.
func (a *App) invalidateGroups() {
	a.GroupRowsOK = false
}

// Rows returns the flattened proxy group tree.
func (a *App) Rows() []GroupRow {
	if a.GroupRowsOK {
		return a.GroupRows
	}
	rows := make([]GroupRow, 0, len(a.GroupNames))
	for groupIndex, name := range a.GroupNames {
		rows = append(rows, GroupRow{GroupIndex: groupIndex})
		if !a.GroupExpanded(name) {
			continue
		}
		group := a.Proxies[name]
		for itemIndex := range group.All {
			rows = append(rows, GroupRow{GroupIndex: groupIndex, ItemIndex: itemIndex, IsItem: true})
		}
	}
	a.GroupRows = rows
	a.GroupRowsOK = true
	return rows
}

// GroupExpanded reports whether a group tree node is expanded.
//
// The server does not persist the expand state, so the dashboard keeps
// its own toggle and only falls back to the server value.
func (a *App) GroupExpanded(name string) bool {
	if expanded, ok := a.Expanded[name]; ok {
		return expanded
	}
	return a.Proxies[name].IsGroup()
}

// ToggleGroup flips the expand state of a group.
func (a *App) ToggleGroup(name string) {
	a.Expanded[name] = !a.GroupExpanded(name)
	a.invalidateGroups()
	a.clampGroupCursor()
}

func (a *App) clampGroupCursor() {
	count := len(a.Rows())
	if count == 0 {
		a.GroupCursor = 0
		return
	}
	if a.GroupCursor >= count {
		a.GroupCursor = count - 1
	}
	if a.GroupCursor < 0 {
		a.GroupCursor = 0
	}
}

// SelectedGroupRow returns the row under the cursor.
func (a *App) SelectedGroupRow() (GroupRow, bool) {
	rows := a.Rows()
	if a.GroupCursor < 0 || a.GroupCursor >= len(rows) {
		return GroupRow{}, false
	}
	return rows[a.GroupCursor], true
}
