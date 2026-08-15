# sing-box Dashboard Design

## 1. Project Overview

This project is a Tauri 2 desktop dashboard for sing-box 1.14+.

The application connects to the sing-box API service, which is a gRPC service that also accepts gRPC-Web requests. It provides:

- Overview statistics and bandwidth history
- Proxy group and selector management
- Live connection inspection and connection closing
- Log viewing and clearing
- Clash mode switching
- System tray integration
- Tray bandwidth display
- Proxy selector in the tray menu
- Close-to-tray behavior

The default API endpoint is:

```text
http://localhost:9000
```

The first launch automatically attempts to connect to this endpoint. The connection URL and API secret are persisted in browser local storage by the frontend.

## 2. Repository Layout

```text
singbox-dashboard/
├── DESIGN.md
├── package.json
├── vite.config.ts
├── src/
│   ├── App.svelte
│   ├── app.css
│   ├── main.ts
│   ├── lib/
│   │   ├── api.ts
│   │   ├── format.ts
│   │   ├── stores.ts
│   │   └── types.ts
│   └── views/
│       ├── Connections.svelte
│       ├── Groups.svelte
│       ├── Logs.svelte
│       ├── Overview.svelte
│       └── Settings.svelte
└── src-tauri/
    ├── Cargo.toml
    ├── build.rs
    ├── tauri.conf.json
    ├── capabilities/default.json
    ├── proto/started_service.proto
    └── src/
        ├── lib.rs
        ├── main.rs
        ├── state.rs
        ├── commands/
        ├── grpc/
        └── tray/
```

## 3. Runtime Architecture

```text
┌─────────────────────────────────────────────┐
│ Tauri application                            │
│                                             │
│  Svelte frontend                            │
│  - views                                     │
│  - stores                                    │
│  - Tauri invoke/listen                       │
│              ▲                 │             │
│              │ events/commands │             │
│              │                 ▼             │
│  Rust backend                              │
│  - gRPC-Web client                          │
│  - background status monitor                 │
│  - tray proxy monitor                        │
│  - tray menu and title                       │
└──────────────────────┬──────────────────────┘
                       │ HTTP/1.1 gRPC-Web
                       ▼
              sing-box API :9000
```

The Rust backend owns the sing-box connection and the system tray. The frontend does not connect to sing-box directly. Frontend commands call Rust through Tauri IPC, and the Rust backend emits events for real-time status updates.

## 4. sing-box API Protocol

The sing-box 1.14 API is not REST. It is the `daemon.StartedService` gRPC service with gRPC-Web support.

The client sends requests to paths such as:

```text
http://localhost:9000/daemon.StartedService/GetVersion
http://localhost:9000/daemon.StartedService/SubscribeStatus
```

Required request headers:

```text
Content-Type: application/grpc-web+proto
X-Grpc-Web: 1
Authorization: Bearer <secret>    # only when a secret is configured
```

The API secret is configured in sing-box's API service configuration. Empty secret means authentication is disabled.

### gRPC-Web frame format

Every request and response message uses this frame:

```text
1 byte  flags
4 bytes message length, big-endian
```

Response flags:

- `0x00`: protobuf data message
- `0x80`: trailers / gRPC status metadata

The Rust implementation manually handles this framing because the sing-box endpoint is gRPC-Web, while the desktop client uses `reqwest` rather than a native HTTP/2 tonic transport.

## 5. Rust Backend

### 5.1 `src-tauri/src/grpc/client.rs`

`GrpcClient` owns:

- A reusable `reqwest::Client`
- API base URL
- Optional bearer secret

The HTTP client is configured with:

- `.no_proxy()` so localhost API access does not accidentally go through the system proxy
- 5 second connection timeout
- 10 second request timeout

The stream reader uses a total deadline, not a per-message deadline. This distinction is critical.

Bad behavior:

```text
wait up to 1.5 seconds for every next message
```

For `SubscribeStatus` and `SubscribeConnections`, sing-box sends a message approximately every second, so this would never return.

Correct behavior:

```text
read messages for a total of 1.5 seconds, then return collected messages
```

The stream reader also limits memory usage:

- Maximum pending stream buffer: 16 MiB
- Maximum decoded stream messages per call: 4096

### 5.2 Current backend API methods

`GrpcClient` currently implements:

- `get_version`
- `subscribe_service_status`
- `subscribe_status`
- `subscribe_groups`
- `subscribe_connections`
- `subscribe_log`
- `clear_logs`
- `get_started_at`
- `select_outbound`
- `url_test`
- `set_group_expand`
- `close_connection`
- `close_all_connections`
- `get_clash_mode_status`
- `set_clash_mode`

Only APIs needed by the current application are kept in the local proto definition.

### 5.3 `src-tauri/src/state.rs`

`AppState` is managed by Tauri and contains:

- Current `ApiConfig`
- Optional shared `Arc<GrpcClient>`
- `quitting` flag used by close-to-tray behavior

The client is stored behind `RwLock<Option<Arc<GrpcClient>>>`. Commands clone the `Arc` while holding a read lock, then perform network work after the lock guard is released.

### 5.4 Tauri commands

Commands are grouped under `src-tauri/src/commands/`:

`status.rs`:

- `connect`
- `disconnect`
- `get_status`
- `get_version`
- `get_started_at`
- `get_service_status`
- `get_clash_mode_status`
- `set_clash_mode`

`groups.rs`:

- `get_groups`
- `select_outbound`
- `url_test`
- `set_group_expand`

`connections.rs`:

- `get_connections`
- `close_connection`
- `close_all_connections`

`logs.rs`:

- `get_logs`
- `clear_logs`

Rust parameter names use snake_case. Tauri exposes command arguments to JavaScript in camelCase by default. For example:

```rust
async fn select_outbound(group_tag: String, outbound_tag: String, ...)
```

is called from JavaScript as:

```ts
invoke("select_outbound", {
  groupTag,
  outboundTag,
});
```

## 6. Background Tasks

### 6.1 Status monitor

`spawn_status_monitor` in `src-tauri/src/lib.rs`:

1. Reads the current shared client from `AppState`
2. Calls `SubscribeStatus` for a bounded interval
3. Converts the latest status to `StatusInfo`
4. Emits `status-update`
5. Updates the tray title with upload/download speed
6. Emits `connection-state` when the API becomes available or unavailable

The status monitor continues retrying after failures. Reconnection is implicit because the client is stateless and each status cycle creates a new request.

### 6.2 Tray proxy monitor

`spawn_tray_proxy_monitor` polls `SubscribeGroups` periodically.

When the serialized group state changes, it rebuilds the tray menu. The signature includes selected nodes, group structure, and delay values, so the checkmark and latency labels update after a selection or URL test.

## 7. Tray Design

The tray is implemented in `src-tauri/src/tray/mod.rs`.

### Static menu items

- Show Dashboard
- Hide Dashboard
- Quit

Closing the main window hides it instead of exiting. The Quit menu sets `AppState.quitting = true` and then exits the app.

### Proxy selector menu

The dynamically rebuilt menu has this structure:

```text
Show Dashboard
Hide Dashboard
----------------
Proxy
├── select_out
│   ├── checked jp_vless_out
│   ├── jp_hy2_out
│   └── ...
└── another_group
    └── ...
----------------
Quit
```

Selectable node IDs use this format:

```text
proxy:<group-tag>:<outbound-tag>
```

The menu event handler parses the ID and calls `GrpcClient::select_outbound` asynchronously.

### Tray title

The current compact format is:

```text
↑1.2M ↓3.4M
```

`M` means MiB/s-style binary units based on 1024-byte conversion. The macOS menu bar title uses the system font; Tauri/tray-icon does not expose a portable font-size setting.

## 8. Frontend Design

### `src/lib/api.ts`

This is the only frontend API wrapper. It should contain thin `invoke()` wrappers and no presentation logic.

### `src/lib/stores.ts`

Global stores:

- `connected`
- `version`
- `serviceStatus`
- `status`
- `groups`
- `startedAt`
- `statusHistory`

Status history is capped at 60 points to keep chart memory bounded.

### Views

`Overview.svelte`:

- Listens for `status-update`
- Displays current upload/download speeds
- Displays total traffic, memory, goroutines, connection counts
- Keeps a 60-point SVG chart history

`Groups.svelte`:

- Polls groups periodically
- Supports selector changes
- Supports URL testing
- Supports group expansion
- Displays Clash mode selector when available

`Connections.svelte`:

- Polls connection event snapshots
- Uses a reactive plain object keyed by connection ID
- Handles NEW, UPDATE, and CLOSED events
- Supports closing one or all connections

`Logs.svelte`:

- Polls log snapshots
- Supports level filtering
- Supports auto-scroll
- Supports Clear
- Supports keeping the latest 200 entries or unlimited local history

`Settings.svelte`:

- Configures API URL and secret
- Connects and disconnects
- Persists connection configuration in `localStorage`
- Toggles light/dark theme

## 9. Important Data Semantics

### Status rate vs total

From the sing-box `Status` message:

- `uplink` / `downlink`: current byte rate
- `uplink_total` / `downlink_total`: cumulative byte count

The tray displays `uplink` and `downlink`, not cumulative totals.

### Connection event types

The numeric enum values are:

```text
0 = NEW
1 = UPDATE
2 = CLOSED
```

Do not change these frontend comparisons without also checking the proto enum.

### Timestamps

Connection timestamps are serialized as numeric values from protobuf `int64`. The frontend treats them as JavaScript numbers and uses them for duration display.

## 10. Build and Verification

From the project root:

```bash
pnpm install
pnpm run check
pnpm build
pnpm tauri dev
pnpm tauri build
```

Rust-only checks:

```bash
cd src-tauri
cargo check
cargo build
```

The local sing-box API should be running at `localhost:9000` for runtime verification.

## 11. Adding a New API Method

Follow this order:

1. Add only the required RPC/message definitions to `src-tauri/proto/started_service.proto`.
2. Run `cargo check` so `tonic-build` regenerates the protobuf types.
3. Add a typed method to `grpc/client.rs`.
4. Convert protobuf data into a serializable application DTO in `state.rs`.
5. Expose a Tauri command in the appropriate `commands` module.
6. Register the command in `src-tauri/src/lib.rs`.
7. Add a thin wrapper in `src/lib/api.ts`.
8. Add or update a frontend store/view.
9. Run both Rust and frontend checks.

Avoid adding the complete upstream proto unless the feature really needs it. The local proto is intentionally smaller than the official dashboard proto.

## 12. Known Limitations and Future Improvements

- Server-streaming is currently implemented as bounded snapshot reads rather than one persistent stream per feature.
- The status and tray group monitors each create periodic HTTP requests. A future optimization could use one persistent Rust subscription manager and fan out cached data to commands, events, and tray.
- The tray title font size follows the operating system. A literal smaller macOS font would require platform-specific Objective-C/AppKit integration.
- Log `Infinite` mode intentionally allows unbounded frontend memory growth. It should be used only for short debugging sessions; a future implementation could use a virtualized list or a disk-backed log store.
- `serde_json::to_string(groups)` is used as a simple tray menu change signature. A custom lightweight signature would reduce allocation if group updates become frequent.
- The generated protobuf types still include standard prost/tonic support code, but unused application-level RPCs and messages have been removed from the local proto.

## 13. Modification Guidelines

- Preserve the gRPC-Web framing and total stream deadline behavior.
- Do not replace `no_proxy()` without considering localhost API access when a system proxy is active.
- Keep network calls out of Tauri synchronous menu callbacks; spawn async work instead.
- Release `RwLock` guards before awaiting network requests when possible.
- Keep frontend API wrappers thin and keep formatting in `format.ts`.
- Use `$state`, `$derived`, and `$effect` consistently in Svelte 5 runes mode.
- Keep bounded histories and connection collections unless an explicit unlimited mode is required.
- Run `cargo check` and `pnpm run check` after structural changes.
