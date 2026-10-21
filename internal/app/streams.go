package app

import (
	"context"
	"net/url"
	"time"

	"mihomo-tui/internal/clash"
)

// streamSpec describes one websocket stream of the controller.
type streamSpec struct {
	name  string
	path  string
	query url.Values
	read  func(context.Context, *clash.Stream) (Event, bool)
}

// streamSpecs returns the streams the dashboard keeps open.
//
// Traffic, memory and connections are pushed by the kernel, the log
// stream is a plain event feed.
func (a *App) streamSpecs() []streamSpec {
	return []streamSpec{
		{name: "traffic", path: "/traffic", read: readTraffic},
		{name: "memory", path: "/memory", read: readMemory},
		{name: "connections", path: "/connections", query: url.Values{"interval": []string{"1000"}}, read: readConnections},
		{name: "logs", path: "/logs", query: logQuery(), read: readLog},
	}
}

// startStreams opens every stream of the current connection.
func (a *App) startStreams(gen uint64) {
	client := a.client
	if client == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	a.cancel = cancel
	for _, spec := range a.streamSpecs() {
		go a.supervise(ctx, gen, client, spec)
	}
}

// supervise keeps one stream alive.
//
// The kernel restarts itself on demand and drops connections when the
// configuration is reloaded, so a stream has to reconnect on its own.
func (a *App) supervise(ctx context.Context, gen uint64, client *clash.Client, spec streamSpec) {
	backoff := time.Second
	for ctx.Err() == nil {
		stream, err := client.DialStream(ctx, spec.path, spec.query)
		if err != nil {
			post(ctx, a.events, Event{Kind: EventStream, Gen: gen, Stream: spec.name, StreamUp: false})
			if !sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		post(ctx, a.events, Event{Kind: EventStream, Gen: gen, Stream: spec.name, StreamUp: true})
		backoff = time.Second

		for ctx.Err() == nil {
			event, ok := spec.read(ctx, stream)
			if !ok {
				break
			}
			event.Gen = gen
			post(ctx, a.events, event)
		}
		stream.Close()
		post(ctx, a.events, Event{Kind: EventStream, Gen: gen, Stream: spec.name, StreamUp: false})
		if !sleep(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

// post delivers an event unless the stream was cancelled.
func post(ctx context.Context, channel chan<- Event, event Event) {
	select {
	case channel <- event:
	case <-ctx.Done():
	}
}

// sleep waits for the duration and reports whether the context survived.
func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > 10*time.Second {
		return 10 * time.Second
	}
	return next
}

func readTraffic(ctx context.Context, stream *clash.Stream) (Event, bool) {
	var traffic clash.Traffic
	if err := stream.ReadJSON(ctx, &traffic); err != nil {
		return Event{}, false
	}
	return Event{Kind: EventTraffic, Traffic: &traffic}, true
}

func readMemory(ctx context.Context, stream *clash.Stream) (Event, bool) {
	var memory clash.Memory
	if err := stream.ReadJSON(ctx, &memory); err != nil {
		return Event{}, false
	}
	inuse := memory.Inuse
	return Event{Kind: EventMemory, Memory: &inuse}, true
}

func readConnections(ctx context.Context, stream *clash.Stream) (Event, bool) {
	var snapshot clash.ConnectionsSnapshot
	if err := stream.ReadJSON(ctx, &snapshot); err != nil {
		return Event{}, false
	}
	return Event{Kind: EventConnections, Snapshot: &snapshot}, true
}

func readLog(ctx context.Context, stream *clash.Stream) (Event, bool) {
	var entry clash.LogEntry
	if err := stream.ReadJSON(ctx, &entry); err != nil {
		return Event{}, false
	}
	return Event{Kind: EventLog, Log: &entry}, true
}

// logQuery returns the server side log level filter.
//
// mihomo skips every record whose level is below the requested one, so
// "debug" is the value that forwards everything the kernel produced.
func logQuery() url.Values {
	return url.Values{"level": []string{"debug"}}
}
