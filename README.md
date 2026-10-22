# mihomo-tui

A terminal dashboard for the [mihomo](https://github.com/MetaCubeX/mihomo) kernel.
It talks to the mihomo external controller over the Clash RESTful API and
pushes live traffic, memory, connection and log data over WebSocket.

The program is a single static Go binary. It needs no WebView, no desktop
toolchain and no CGO, so it also runs on old Linux installations.

```text
┌──────────────── mihomo TUI v1.19.27 ───────────────── [connected] ┐
│ [1] Overview  [2] Proxy Groups  [3] Connections  [4] Logs  ...   │
├─────────────────────────────────────────────────────────────────┤
│ Uplink rate │ Downlink rate │ Total upload │ Total download      │
│ Bandwidth sparkline (60 samples)                                 │
│ Kernel memory │ Connections │ Mode │ Log level                  │
└─────────────────────────────────────────────────────────────────┘
```

## Features

- **Overview** — uplink and downlink rate, cumulative traffic, kernel
  memory, connection count, the runtime mode and a 60 sample bandwidth
  sparkline
- **Proxy Groups** — the policy group tree with the active node of every
  group, node latency, manual node switching and latency tests for a
  single node or a whole group
- **Connections** — every tracked connection with source, destination,
  proxy chain, matched rule, transferred bytes and age, plus a detail
  panel and the ability to close one or all connections
- **Logs** — the live kernel log over a WebSocket stream with a severity
  filter, follow mode and scrolling
- **Settings** — controller URL, bearer secret and configuration path,
  runtime mode switching, configuration reload, kernel restart and cache
  flushing
- **Servers** — an optional overview of several monitored instances, and
  a modal overlay with the connections and the log records of an opened
  instance

## Development

```bash
make build      # static binary without CGO
make test       # run the tests
make race       # run the tests with the race detector
make lint       # go vet, gofmt -s, staticcheck and deadcode
make check      # lint, test and race
```

The lint step needs two tools that are not part of the standard library:

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
go install golang.org/x/tools/cmd/deadcode@latest
```

`staticcheck` reports the usual code smells, `deadcode` finds functions
that are never called.

## Build

Go 1.21 or newer is required.

```bash
go build -trimpath -o mihomo-tui .
```

For a static build without CGO, which is the recommended variant for old
Linux systems:

```bash
CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o mihomo-tui .
```

The bundled script does the same and honours `GOOS`, `GOARCH` and `OUTPUT`:

```bash
./build-local.sh
GOOS=linux GOARCH=arm64 OUTPUT=mihomo-tui-arm64 ./build-local.sh
```

## Run

```bash
./mihomo-tui
./mihomo-tui --url http://127.0.0.1:9090
./mihomo-tui --url http://127.0.0.1:9090 --secret 'your-secret'
```

The controller must be enabled in the mihomo configuration:

```yaml
external-controller: 127.0.0.1:9090
secret: "your-secret"      # optional
```

The configuration precedence is defaults, configuration file, environment
variables and finally command line flags.

```bash
MIHOMO_API_URL=http://127.0.0.1:9090 \
MIHOMO_API_SECRET=your-secret \
./mihomo-tui
```

`--no-connect` starts the dashboard without connecting, so the URL and the
secret can be edited on the Settings page first. The secret is stored in
`~/.config/mihomo-tui/config` with `0600` permissions.

## Keys

```text
1..6       switch pages
Tab        next page
r          refresh every snapshot
?          help
q/Ctrl-C   quit
```

| Page | Keys |
| --- | --- |
| Overview | `[` `]` `m` switch between rule, global and direct |
| Proxy Groups | Up/Down, `Enter` switch node, `t` test node, `T` test group, `x` expand |
| Connections | Up/Down, `Enter` details, `c` close, `C` close all, `s` sort |
| Logs | Up/Down, PgUp/PgDn, Home/End, `f` filter, `a` follow, `c` clear |
| Settings | Up/Down, `Enter`/`e` edit, `c` connect, `d` disconnect, `R` reload, `X` restart, `F` flush dns, `P` flush fake-ip |
| Servers | Up/Down, `Enter` open an instance in an overlay, PgUp/PgDn, Home/End |

Opening an instance with `Enter` shows a modal overlay with the
connections and the log records of that instance. The pages below keep
showing the local kernel, the overlay only covers them while it is open.
`Esc` closes the overlay again.

## Monitoring several instances

The Servers page reads a plain text file with one instance per line. The
format is `name address [protocol]`, `#` starts a comment and a missing
scheme is completed with `http://`.

```text
# name  address                 protocol
hk      43.128.1.131:9000
jp      jp.drawdark.site:9000
mihomo  192.168.1.10:9090       clash
```

The protocol defaults to `sing-box`, the only other accepted value is
`clash`. A sing-box instance is monitored over its gRPC-Web API, a mihomo
instance over the RESTful API of the controller.

```bash
./mihomo-tui --servers ~/.config/mihomo-tui/servers
```

Without `--servers` the page stays empty and does not affect the other
pages. The monitored instances use no secret.

### Opening an instance

The Servers page can open an instance to inspect it. A modal overlay
appears with the connections and the log records of the instance:

```text
  +- jp  1.14.0-rc.1   [Esc closes] ---------------------------+
  | Connections: 10 active  UP 0 B  DN 0 B                     |
  | Destination        Proto  Chain  Rule   Upload  Download  |
  | www.google.com           0                        0 B  0 B |
  | ---------------------------------------------------------- |
  | +- Connection details ------------------------------------+ |
  | | tcp 10.0.0.1:51000 -> www.google.com                    | |
  | | network: tcp   dns: -   inbound: 2 (vless)              | |
  | | rule: - (-)                                            | |
  | | chain: 0                                               | |
  | | upload: 0 B   download: 1.28 KB                        | |
  | | started: 2026-10-22T09:10:12+08:00                     | |
  | | id: 0aec31ac-5bb6-44e4-b4ee-09a30a9b7ef7               | |
  | +--------------------------------------------------------+ |
  | ---------------------------------------------------------- |
  | Logs: 0 shown, filter all [follow]                        |
  +-------------------------------------------------------------+
```

The detail panel shows the same lines as the one of the local kernel.
A terminal that is too small for every line falls back to a short form
of the panel, and the log records give way to it.

The overlay is modal, so the keys belong to it while it is open:

| Key | Action |
| --- | --- |
| Up/Down | move the cursor in the connection list |
| `Enter` | show or hide the details of the selected connection |
| PgUp/PgDn, Home/End | scroll the log records |
| `f` | cycle the log level filter |
| `a` | follow the newest log record |
| `c` | close the selected connection of the instance |
| `C` | close every connection of the instance |
| `Esc` | close the overlay |

The sing-box API reports the connections as events: the first message of
the subscription carries the current state, the following ones only the
changes. The streams are event driven, so an idle instance stays silent
until the deadline and the subscription is opened again afterwards.

### The sing-box protocol

sing-box does not offer a RESTful API, it exposes the
`daemon.StartedService` gRPC service over gRPC-Web:

```text
POST http://host:9000/daemon.StartedService/GetVersion
POST http://host:9000/daemon.StartedService/SubscribeStatus

Content-Type: application/grpc-web+proto
X-Grpc-Web: 1
```

The dashboard keeps the status subscription open, so the traffic of every
instance arrives without polling. A sample reports the kernel memory, the
goroutine count, the number of connections and the transferred bytes.

The `uplink` and `downlink` fields of the status are the number of bytes
transferred during the last push interval, so the dashboard divides them by
the interval to get a rate. The first sample of a subscription never
carries a rate, because sing-box computes the difference only after the
first tick.

## API coverage

### mihomo (the main connection)

| Endpoint | Usage |
| --- | --- |
| `GET /version` | kernel version in the title bar |
| `GET /configs` | runtime mode, log level and ports |
| `PATCH /configs` | switch between rule, global and direct |
| `PUT /configs?force=true` | reload the configuration file |
| `POST /restart` | restart the kernel |
| `GET /proxies` | policy groups, nodes and latency history |
| `PUT /proxies/{name}` | switch the active node of a selector |
| `DELETE /proxies/{name}` | clear the pinned node of an automatic group |
| `GET /proxies/{name}/delay` | latency of a single node |
| `GET /group/{name}/delay` | latency of every node of a group |

The latency test uses the health check URL of the group, which mihomo
reports as `testUrl`. Groups without a configured URL fall back to
`https://www.gstatic.com/generate_204`, the default of mihomo itself. The
timeout is five seconds.
| `GET /traffic` (WS) | live rate and cumulative traffic |
| `GET /memory` (WS) | kernel memory |
| `GET /connections` (WS) | live connection snapshots |
| `DELETE /connections[/{id}]` | close one or all connections |
| `GET /logs` (WS) | live kernel log |
| `POST /cache/dns/flush` | clear the resolver cache |
| `POST /cache/fakeip/flush` | clear the fake-ip cache |

### sing-box (the monitored instances)

| Endpoint | Usage |
| --- | --- |
| `GetVersion` (gRPC-Web) | version of the instance |
| `SubscribeStatus` (gRPC-Web stream) | memory, goroutines, connections and traffic |
| `SubscribeConnections` (gRPC-Web stream) | the connections of an opened instance |
| `SubscribeLog` (gRPC-Web stream) | the log records of an opened instance |
| `CloseConnection`, `CloseAllConnections` | close the connections of an opened instance |


See [DESIGN.md](DESIGN.md) for the internal architecture.
