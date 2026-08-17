# sing-box Dashboard

基于 **Tauri 2** + **Svelte 5** 的 sing-box（1.14+）桌面管理面板。

应用通过 gRPC-Web 协议连接 sing-box API 服务，提供概览统计、代理组管理、实时连接查看、日志查看、Clash 模式切换等功能，并集成系统托盘。

## 功能特性

- 概览统计与带宽历史图表
- 代理组与节点选择管理
- 实时连接监控与连接关闭
- 日志查看与清空
- Clash 模式切换
- 系统托盘集成：托盘带宽显示、托盘代理选择菜单、关闭窗口不退出

默认 API 地址为 `http://localhost:9000`，首次启动会自动尝试连接。连接地址与 API 密钥由前端持久化在浏览器 `localStorage` 中。

## 架构

```text
┌─────────────────────────────────────────────┐
│ Tauri 应用                                  │
│                                             │
│  Svelte 前端                                │
│  - views / stores                          │
│  - Tauri invoke/listen                     │
│              ▲                 │             │
│              │ events/commands │             │
│              │                 ▼             │
│  Rust 后端                                 │
│  - gRPC-Web 客户端                          │
│  - 后台状态监控                             │
│  - 托盘代理监控 / 托盘菜单与标题             │
└──────────────────────┬──────────────────────┘
                       │ HTTP/1.1 gRPC-Web
                       ▼
              sing-box API :9000
```

- Rust 后端持有 sing-box 连接与系统托盘，前端不直接连接 sing-box。
- 前端通过 Tauri IPC 调用命令，后端通过事件（如 `status-update`）推送实时状态。

## 目录结构

```text
singbox-dashboard/
├── DESIGN.md              # 详细设计文档
├── package.json
├── vite.config.ts
├── src/
│   ├── App.svelte
│   ├── app.css
│   ├── main.ts
│   ├── lib/
│   │   ├── api.ts         # 唯一的 invoke() 薄封装
│   │   ├── format.ts      # 格式化工具
│   │   ├── stores.ts      # 全局 store（状态历史上限 60 点）
│   │   └── types.ts
│   └── views/
│       ├── Overview.svelte
│       ├── Groups.svelte
│       ├── Connections.svelte
│       ├── Logs.svelte
│       └── Settings.svelte
└── src-tauri/
    ├── Cargo.toml
    ├── tauri.conf.json
    ├── capabilities/default.json
    ├── proto/started_service.proto   # 裁剪后的本地 proto 定义
    └── src/
        ├── lib.rs                    # 命令注册、后台监控任务
        ├── main.rs
        ├── state.rs                  # AppState 与 DTO
        ├── commands/                 # status / groups / connections / logs
        ├── grpc/                     # GrpcClient（gRPC-Web 客户端）
        └── tray/                     # 系统托盘
```

## 开发与构建

前置条件：Node.js + pnpm、Rust 工具链，以及运行在 `localhost:9000` 的 sing-box API。

```bash
pnpm install
pnpm run check     # 前端类型检查
pnpm build         # 前端构建
pnpm tauri dev     # 开发模式
pnpm tauri build   # 打包
```

仅 Rust 检查：

```bash
cd src-tauri
cargo check
cargo build
```

## 设计要点

### gRPC-Web 协议

sing-box 1.14 的 API 不是 REST，而是支持 gRPC-Web 的 `daemon.StartedService` 服务，例如：

```text
http://localhost:9000/daemon.StartedService/GetVersion
http://localhost:9000/daemon.StartedService/SubscribeStatus
```

请求头：

```text
Content-Type: application/grpc-web+proto
X-Grpc-Web: 1
Authorization: Bearer <secret>    # 仅当配置了密钥时
```

消息帧格式为 `1 字节 flags + 4 字节大端长度`，响应 flags：`0x00` 为 protobuf 数据，`0x80` 为 trailers。由于目标端点是 gRPC-Web 而非原生 HTTP/2，Rust 端使用 `reqwest` 手动处理帧。

### 流式读取

服务端流采用**有界快照读取**而非单条消息超时：

- 在总时长内（如 1.5 秒）持续读取并收集消息，随后一次性返回；
- 单条消息超时会因 sing-box 约每秒一条的推送而永久阻塞；
- 流缓冲上限 16 MiB，单次调用最多解码 4096 条消息。

### 托盘

- 静态项：Show Dashboard / Hide Dashboard / Quit；关闭窗口隐藏而非退出。
- 动态代理选择菜单：节点 ID 格式为 `proxy:<group-tag>:<outbound-tag>`，状态变化（选中节点、组结构、延迟）会重建菜单。
- 托盘标题显示实时速率，格式如 `↑1.2M ↓3.4M`（基于 1024 的二进制单位）。

## 扩展：新增 API 方法

1. 在 `src-tauri/proto/started_service.proto` 只添加所需 RPC/消息定义；
2. 运行 `cargo check` 让 `tonic-build` 重新生成 protobuf 类型；
3. 在 `grpc/client.rs` 添加类型化方法；
4. 在 `state.rs` 将 protobuf 数据转换为可序列化 DTO；
5. 在对应 `commands` 模块暴露 Tauri 命令并在 `src-tauri/src/lib.rs` 注册；
6. 在 `src/lib/api.ts` 添加薄封装，再更新 store/视图；
7. 同时运行 Rust 与前端检查。

> 注意：本地 proto 刻意小于官方 dashboard 的完整 proto，只保留应用实际用到的部分。

## 已知限制

- 服务端流以有界快照轮询实现，而非每条特性一条持久流；
- 日志 `Infinite` 模式允许无界内存增长，仅建议短期调试使用；
- 托盘标题字体大小跟随系统，无法通过 Tauri 直接调整。

## 旧 Linux：Go TUI

如果目标系统无法运行 Tauri 或 Rust 工具链，使用同目录下的
`singbox-go-tui/`。这是一个不依赖 WebView、`protoc` 和 CGO 的 Go 终端版，
直接连接同一个 sing-box 1.14+ gRPC-Web API，提供概览、代理组、连接、日志和
设置页面。

```bash
cd singbox-go-tui
./build-local.sh
./singbox-go-tui --url http://127.0.0.1:9000
```

详细构建方式和快捷键见 `singbox-go-tui/README.md`。
