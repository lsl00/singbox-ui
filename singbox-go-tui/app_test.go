package main

import (
	"errors"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestStaleGroupsSnapshotDiscarded(t *testing.T) {
	app := newApp(APIConfig{URL: defaultAPIURL})
	client, err := newAPIClient(APIConfig{URL: defaultAPIURL})
	if err != nil {
		t.Fatal(err)
	}
	app.client = client
	tx := make(chan NetworkEvent, 8)

	stale := []GroupInfo{{Tag: "proxy", IsExpand: false}}
	fresh := []GroupInfo{{Tag: "proxy", IsExpand: true}}

	app.groupsEpoch = 5
	app.pollGroups(tx)
	if !app.groupsBusy {
		t.Fatalf("poll should be in flight")
	}
	// The mutation completes before the snapshot above returns.
	var event NetworkEvent
	for {
		event = <-tx
		if event.Kind == NetworkGroups {
			break
		}
	}
	if event.Epoch != 5 {
		t.Fatalf("unexpected epoch: %d", event.Epoch)
	}
	event.Groups = fresh
	app.groupsEpoch = 6
	app.handleNetwork(event, tx)
	if len(app.groups) != 1 || !app.groups[0].IsExpand {
		t.Fatalf("fresh state not applied: %+v", app.groups)
	}

	oldSnapshot := NetworkEvent{
		Kind:       NetworkGroups,
		Generation: app.generation,
		Groups:     stale,
		Epoch:      4,
	}
	app.handleNetwork(oldSnapshot, tx)
	if len(app.groups) != 1 || !app.groups[0].IsExpand {
		t.Fatalf("stale snapshot overwrote newer state: %+v", app.groups)
	}

	sameSnapshot := oldSnapshot
	sameSnapshot.Epoch = 6
	sameSnapshot.Groups = []GroupInfo{{Tag: "proxy", IsExpand: false}, {Tag: "auto"}}
	app.handleNetwork(sameSnapshot, tx)
	if len(app.groups) != 2 {
		t.Fatalf("snapshot with current epoch should apply: %+v", app.groups)
	}
}

func TestStartActionReportsBusyAndRunsWhenFree(t *testing.T) {
	app := newApp(APIConfig{URL: defaultAPIURL})
	client, err := newAPIClient(APIConfig{URL: defaultAPIURL})
	if err != nil {
		t.Fatal(err)
	}
	app.client = client

	tx := make(chan NetworkEvent, 8)
	called := false
	action := func(*APIClient) error { called = true; return nil }

	app.actionBusy = true
	app.startAction(tx, ActionExpandGroup, "", action)
	if called {
		t.Fatalf("action must not run while busy")
	}
	if app.message != "previous action still in progress" {
		t.Fatalf("busy drop should be visible, got %q", app.message)
	}
	select {
	case event := <-tx:
		t.Fatalf("no event expected while busy, got %+v", event)
	default:
	}

	app.actionBusy = false
	app.startAction(tx, ActionExpandGroup, "", action)
	select {
	case event := <-tx:
		if event.Kind != NetworkAction || event.Action != ActionExpandGroup || event.Err != nil {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatalf("action event missing")
	}
	if !called {
		t.Fatalf("action should have run")
	}
}

func TestLocalExpandToggleOverridesServerState(t *testing.T) {
	app := newApp(APIConfig{URL: defaultAPIURL})
	client, err := newAPIClient(APIConfig{URL: defaultAPIURL})
	if err != nil {
		t.Fatal(err)
	}
	app.client = client
	// Server reports collapsed (e.g. no cache_file configured).
	app.groups = []GroupInfo{
		{Tag: "proxy", IsExpand: false, Items: []GroupItemInfo{{Tag: "a"}, {Tag: "b"}}},
	}

	tx := make(chan NetworkEvent, 8)
	app.handleGroupsKey(tcell.NewEventKey(tcell.KeyRune, 'x', 0), tx)

	if !app.groupIsExpanded(app.groups[0]) {
		t.Fatalf("toggle should expand locally")
	}
	rows := app.groupRows()
	if len(rows) != 3 {
		t.Fatalf("expanded group should expose item rows, got %d", len(rows))
	}
	select {
	case event := <-tx:
		if event.Kind != NetworkAction || event.Action != ActionExpandGroup {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatalf("expand action event missing")
	}

	app.handleGroupsKey(tcell.NewEventKey(tcell.KeyRune, 'x', 0), tx)
	if app.groupIsExpanded(app.groups[0]) {
		t.Fatalf("second toggle should collapse again")
	}
	if rows := app.groupRows(); len(rows) != 1 {
		t.Fatalf("collapsed group should hide items, got %d rows", len(rows))
	}
}

func TestGroupIsExpandedPrefersServerWhenNoOverride(t *testing.T) {
	app := newApp(APIConfig{URL: defaultAPIURL})
	group := GroupInfo{Tag: "proxy", IsExpand: true}
	if !app.groupIsExpanded(group) {
		t.Fatalf("server expand state should apply without local override")
	}
	app.expanded["other"] = true
	if app.groupIsExpanded(group) != group.IsExpand {
		t.Fatalf("override for another tag must not leak")
	}
}

func TestStartActionRequiresClient(t *testing.T) {
	app := newApp(APIConfig{URL: defaultAPIURL})
	tx := make(chan NetworkEvent, 8)
	app.startAction(tx, ActionClearLogs, "", func(*APIClient) error { return nil })
	select {
	case event := <-tx:
		t.Fatalf("no event expected without client, got %+v", event)
	default:
	}
	if !app.actionBusy {
		return
	}
	t.Fatalf("busy flag must stay untouched")
}

func TestNetworkActionFailureKeepsState(t *testing.T) {
	app := newApp(APIConfig{URL: defaultAPIURL})
	tx := make(chan NetworkEvent, 8)

	event := NetworkEvent{
		Kind:       NetworkAction,
		Action:     ActionExpandGroup,
		Generation: 1,
		Err:        errors.New("boom"),
	}
	app.generation = 1
	app.handleNetwork(event, tx)
	if app.actionBusy {
		t.Fatalf("busy flag should reset on failure")
	}
	if app.error != "boom" {
		t.Fatalf("expected error surfaced, got %q", app.error)
	}
	if app.groupsEpoch != 0 {
		t.Fatalf("failed action must not bump epoch")
	}
}
