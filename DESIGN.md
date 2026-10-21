# mihomo-tui Design

## 1. Overview

`mihomo-tui` is a terminal dashboard for the mihomo kernel. It connects to
the mihomo external controller over the Clash RESTful API and consumes the
WebSocket streams the controller offers for traffic, memory, connections and
logs.

The design goals are:

- a single static binary that runs on old Linux systems, so no CGO, no
  WebView and no desktop toolchain is involved
- live data without polling, so the kernel pushes the samples
- the same interaction model as the previous sing-box dashboard: a page per
  topic, a shared footer with the key hints and a status line

## 2. Repository layout

```text
.
├── main.go                  # entry point, terminal setup and event loop
├── build-local.sh           # reproducible static build
├── go.mod
└── internal/
    ├── clash/               # mihomo controller client
    │   ├── client.go        # REST calls
    │   ├── stream.go        # WebSocket streams
    │   ├── types.go         # JSON models
    │   └── client_test.go   # tests against a fake controller
    ├── config/              # CLI parsing and persistence
    │   ├── config.go
    │   └── config_test.go
    ├── singbox/             # sing-box gRPC-Web client
    │   ├── client.go        # unary calls and the status stream
    │   ├── proto.go         # gRPC-Web framing and protobuf codec
    │   ├── client_test.go
    │   └── live_test.go     # opt-in check against real instances
    ├── app/                 # dashboard state machine
    │   ├── app.go           # state, event handling, key handling
    │   ├── actions.go       # write operations
    │   ├── events.go        # event and action types
    │   ├── servers.go       # monitored instances
    │   ├── streams.go       # stream supervision
    │   ├── app_test.go
    │   └── integration_test.go
    └── ui/                  # tcell rendering
        ├── ui.go            # frame, header, footer, help
        ├── overview.go
        ├── groups.go
        ├── connections.go
        ├── logs.go
        ├── settings.go
        ├── servers.go
        ├── widgets.go       # boxes, tables, sparkline, formatting
        └── ui_test.go
```

The dependency direction is `main -> ui -> app -> {clash, singbox} ->
net/http`. The `ui` package never performs network calls, the `app` package
never touches the terminal.

## 3. Runtime architecture

```text
┌──────────────────────────── mihomo-tui ───────────────────────────┐
│                                                                  │
│  main loop (single goroutine)                                   │
│    - tcell events        - app events        - periodic ticks     │
│            │                    ▲                    │             │
│            ▼                    │                    ▼             │
│  app.App (state)  ◄────────────┴────────────────────┘             │
│    - pages, cursors, filters                                    │
│                                                                  │
│  network goroutines                                             │
│    - 4 WebSocket supervisors (traffic, memory, connections, logs)│
│    - one-shot REST calls (proxies, configs, server monitors)     │
│    - one gRPC-Web supervisor per monitored sing-box instance     │
└──────────────────────────────────────────────────────────────────┘
                │  HTTP/1.1 + WebSocket
                ▼
        mihomo external controller :9090
```

The main loop is the only place that mutates the state. Network goroutines
send results through a single `app.Event` channel, which keeps the state
free of locks and makes the data flow easy to follow.

### Tick rates

| Timer | Interval | Purpose |
| --- | --- | --- |
| draw | 100 ms | repaint the screen |
| proxies | 3 s | refresh groups, nodes and latency history |
| servers | 2 s | read the next sample of the monitored instances |
| runtime | 15 s | refresh the running configuration |

Traffic, memory, connections and logs are pushed by the kernel and need no
polling at all.

## 4. The controller protocol

The mihomo external controller is a plain HTTP server.

### Authentication

When a secret is configured, every request carries an
`Authorization: Bearer <secret>` header. The controller also accepts a
`token` query parameter for WebSocket upgrades, but the header works for
both transports because the WebSocket client can set handshake headers.

### Streaming

`/traffic`, `/memory`, `/connections` and `/logs` accept a WebSocket
upgrade and then push one JSON document per frame.

- `/traffic` pushes `{"up","down","upTotal","downTotal"}` once per second.
  `up` and `down` are byte rates, `upTotal` and `downTotal` are cumulative
  byte counters.
- `/memory` pushes `{"inuse","oslimit"}` once per second. The controller
  deliberately reports `0` for the very first sample, so the dashboard
  ignores zero values.
- `/connections` pushes a full snapshot once per second. The refresh rate
  is configurable with `?interval=<milliseconds>`.
- `/logs` pushes `{"type","payload"}` per line.

The log level filter deserves a note. The controller drops every record
whose level is below the requested one, and its level constants are
ordered `DEBUG=0, INFO=1, WARNING=2, ERROR=3, SILENT=4`. A request for
`?level=debug` therefore forwards everything the kernel produced, while
the default `?level=info` would drop every warning and error. The
dashboard always requests `?level=debug` and applies the severity filter
itself, which keeps the filter switchable without reconnecting and works
independently of the controller version.

### Write operations

| Operation | Request |
| --- | --- |
| switch mode | `PATCH /configs` with `{"mode":"global"}` |
| reload configuration | `PUT /configs?force=true` with `{"path":"","payload":""}` |
| restart kernel | `POST /restart` with `{"path":"","payload":""}` |
| switch node | `PUT /proxies/{group}` with `{"name":"node"}` |
| test node | `GET /proxies/{node}/delay?url=&timeout=` |
| test group | `GET /group/{group}/delay?url=&timeout=` |
| close connections | `DELETE /connections[/{id}]` |
| flush caches | `POST /cache/dns/flush`, `POST /cache/fakeip/flush` |

Two behaviours of the controller are worth remembering:

- `PUT /configs` with an empty path reloads the configuration file the
  kernel was started with. A path must be absolute and inside the
  `SAFE_PATHS` of the kernel, otherwise the request is rejected.
- `POST /restart` makes the kernel replace itself with a new process. The
  controller answers first and disappears afterwards, so the dashboard has
  to survive a short outage.
- `GET /group/{group}/delay` clears the pinned selection of automatic
  groups, so a group wide test is not free of side effects.

### Proxy and group model

A proxy is a policy group exactly when it carries an `all` list. Groups
add `now`, `testUrl`, `fixed`, `hidden` and `emptyFallback` on top of the
common fields. The dashboard uses `type == "Selector"` to decide whether a
group can be switched manually.

The controller returns the proxies as a map, so the dashboard sorts the
group names alphabetically to get a stable order.

## 5. The sing-box protocol

The monitored instances run sing-box, which does not offer a RESTful API.
It exposes the `daemon.StartedService` gRPC service over gRPC-Web, so the
client has to speak the gRPC-Web framing and decode protobuf by hand.

### Framing

Every request and every response message uses the gRPC-Web frame:

```text
1 byte  flags
4 bytes message length, big-endian
```

The flags distinguish the data frames (`0x00`) from the trailer frames
(`0x80`). A trailer carries the gRPC status, so a failed call surfaces as
an error instead of an empty result.

The requests are:

```text
POST http://host:9000/daemon.StartedService/GetVersion
POST http://host:9000/daemon.StartedService/SubscribeStatus
```

with the headers `Content-Type: application/grpc-web+proto` and
`X-Grpc-Web: 1`. The body of the subscription is the
`SubscribeStatusRequest` with the push interval in nanoseconds.

### Decoding

The protobuf codec is limited to what the dashboard needs. The decoder
walks the fields of a message, keeps the known ones and skips the rest.
Only the `Version` and the `Status` message are decoded, so the codec
stays small and no `protoc` and no generated code are needed.

### The semantics of the status

The `Status` message of sing-box is filled by `readStatus` and adjusted
before every push:

```go
status := s.readStatus()
uploadTotal := status.UplinkTotal
downloadTotal := status.DownlinkTotal
for {
    server.Send(status)
    <-ticker.C
    status = s.readStatus()
    upload := status.UplinkTotal - uploadTotal
    download := status.DownlinkTotal - downloadTotal
    status.Uplink = upload
    status.Downlink = download
}
```

Three details follow from this and are easy to get wrong:

- `uplink` and `downlink` are the number of bytes transferred during the
  last push interval, not a normalised rate. The dashboard divides them by
  the interval to get bytes per second.
- The first message of a subscription never carries a rate, because the
  difference is only computed after the first tick. The probe discards
  that message.
- `connectionsIn` is the number of tracked connections and
  `connectionsOut` the number of outbound connections.

The decoder is verified against this invariant: the reported rate has to
be the difference of the cumulative counters. The live test asserts it for
every sample of every configured instance.

### Reading the stream

The subscription stays open, so the probe keeps it and only reads the next
sample from it. Because the instance pushes a sample every interval and
the dashboard polls less often, the newest sample wins and the older ones
are discarded. Otherwise the reader would show an ever growing backlog.

## 6. State handling

### Generation gate

Every event carries the generation of the connection it belongs to. A
connection or a disconnect increments the generation, and events of an
older generation are dropped. This keeps late responses of a previous
controller from overwriting the state of the current one.

### Busy flags

Each kind of one-shot request has its own busy flag, so a slow response
never leads to overlapping requests. Write operations share a single flag
and are rejected with a visible message while one is running.

### Stream supervision

Each WebSocket stream runs in its own goroutine that reconnects with an
exponential backoff of up to ten seconds. The supervisor is cancelled when
the connection is closed or replaced, which is what makes the kernel
restart and the configuration reload survivable: the streams drop, the
supervisor retries, and the dashboard reconnects on its own.

## 7. Pages

| Page | Content |
| --- | --- |
| Overview | rate and total traffic, bandwidth sparkline, kernel memory, connection count, runtime mode, log level |
| Proxy Groups | mode selector, group tree, node latency, node switching, latency tests |
| Connections | connection table, detail panel, closing connections, sorting |
| Logs | live log stream, severity filter, follow mode, scrolling |
| Settings | controller URL, secret, configuration path, kernel actions |
| Servers | grid of monitored instances with version, rates, totals, memory and connection count |

### Bounded buffers

| Buffer | Limit |
| --- | --- |
| rate history | 60 samples |
| log lines | 2000 lines |
| connections | 5000 entries |

The log buffer keeps every line regardless of the filter, so lowering the
filter reveals the earlier lines again.

### Servers page

The page supports two protocols. The protocol of an instance is part of
its entry in the server list and defaults to sing-box.

A sing-box instance is monitored over its gRPC-Web status stream, which
the kernel pushes on its own. The subscription is opened once and kept
open, so the traffic of the instance arrives without any polling.

A mihomo instance is polled instead. A single `GET /connections` snapshot
already contains the cumulative traffic counters and the kernel memory, so
the transfer rate is derived from the difference between two polls. This
keeps the page cheap even with many instances.

## 8. Configuration

The controller URL and the secret are stored in
`~/.config/mihomo-tui/config` with `0600` permissions and written
atomically. The precedence of the sources is

```text
defaults -> configuration file -> environment -> command line flags
```

The environment variables are `MIHOMO_API_URL` and `MIHOMO_API_SECRET`.

## 9. Testing

```bash
go test ./...
go test -race ./...
```

The tests cover the following:

- the REST and WebSocket client against a fake controller, including the
  escaping of proxy names and the error handling of HTTP failures
- the state machine: the generation gate, the busy flags, the group tree,
  the log filter, the connection sorting and the mode cycling
- an end to end test that connects the dashboard to a fake controller and
  waits for the streamed samples to arrive
- the rendering of every page at several terminal sizes using the
  simulation screen of tcell
- the sing-box client against a fake instance, including the gRPC-Web
  framing, the trailer handling and the rate normalisation

The live check against the real instances is opt-in:

```bash
MIHOMO_TUI_LIVE=1 go test ./internal/singbox/ -run TestLiveInstances -v
```

It reads the instance list from `MIHOMO_TUI_SERVERS` or from the servers
file in the home directory and verifies the rate invariant for every
sample.

## 10. Known limitations

- The connection table is a snapshot of the kernel. Closing a connection
  is reflected after the next snapshot, which takes up to a second.
- The dashboard does not persist the state of the kernel. The mode and the
  node selection live in the kernel and survive a restart of the
  dashboard.
- The `Servers` page ignores secrets, because the instance list format
  has no field for them.
- A sing-box instance reports the transfer rate of the last push interval
  only, so the value can be up to one second old.
- A proxy group whose name contains a slash cannot be addressed, the
  controller itself cannot route such a path either.
- The kernel has to be reachable over plain HTTP or HTTPS. There is no
  support for a Unix socket or a named pipe.
