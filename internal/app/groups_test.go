package app

import (
	"testing"

	"mihomo-tui/internal/clash"
)

func TestGroupsAreSortedWithGlobalLast(t *testing.T) {
	state := newTestApp()
	state.Proxies = map[string]clash.Proxy{
		"GLOBAL": {Name: "GLOBAL", Type: "Selector", All: []string{"A", "B"}},
		"Proxy":  {Name: "Proxy", Type: "Selector", All: []string{"A"}},
		"AUTO":   {Name: "AUTO", Type: "URLTest", All: []string{"A"}},
		"节点选择":   {Name: "节点选择", Type: "Selector", All: []string{"A"}},
		"DIRECT": {Name: "DIRECT", Type: "Direct"},
	}
	state.rebuildGroups()

	want := []string{"AUTO", "Proxy", "节点选择", "GLOBAL"}
	if len(state.GroupNames) != len(want) {
		t.Fatalf("unexpected group list: %+v", state.GroupNames)
	}
	for index, name := range want {
		if state.GroupNames[index] != name {
			t.Fatalf("unexpected group list: %+v", state.GroupNames)
		}
	}
}

func TestGlobalGroupKeepsItsPlaceWhenItIsTheOnlyGroup(t *testing.T) {
	state := newTestApp()
	state.Proxies = map[string]clash.Proxy{
		"GLOBAL": {Name: "GLOBAL", Type: "Selector", All: []string{"A"}},
	}
	state.rebuildGroups()
	if len(state.GroupNames) != 1 || state.GroupNames[0] != "GLOBAL" {
		t.Fatalf("unexpected group list: %+v", state.GroupNames)
	}
}

func TestTestURLPrefersTheGroupConfiguration(t *testing.T) {
	state := newTestApp()
	state.Proxies = map[string]clash.Proxy{
		"Custom": {Name: "Custom", Type: "Selector", All: []string{"A"}, TestURL: "http://www.gstatic.com/generate_204"},
		"Plain":  {Name: "Plain", Type: "Selector", All: []string{"A"}},
	}
	if got := state.testURLFor("Custom"); got != "http://www.gstatic.com/generate_204" {
		t.Fatalf("unexpected test url: %q", got)
	}
	if got := state.testURLFor("Plain"); got != clash.DefaultTestURL {
		t.Fatalf("unexpected test url: %q", got)
	}
	if got := state.testURLFor("Missing"); got != clash.DefaultTestURL {
		t.Fatalf("unexpected test url: %q", got)
	}
}
