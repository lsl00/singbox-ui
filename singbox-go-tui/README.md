# sing-box Go TUI

独立的 Go 终端监控面板，直接连接 sing-box 1.14+ 的 gRPC-Web API。它不依赖
Tauri、Rust、Node.js、WebView、`protoc` 或 CGO，适合无法运行桌面版的旧 Linux。

## 功能

- Overview：上下行速率、累计流量、内存、goroutine、连接数、运行时间和速率历史
- Proxy Groups：选择节点、URL 测试、展开/折叠代理组、切换 Clash mode
- Connections：实时连接列表、连接详情、关闭单个或全部连接
- Logs：等级过滤、滚动/跟随、清空日志，最多保留 2000 条本地记录
- Settings：编辑 API 地址和 secret，连接配置保存到
  `~/.config/singbox-go-tui/config`

## 构建

需要 Go 1.20 或更新版本。普通构建：

```bash
go build -trimpath -o singbox-go-tui .
```

旧 Linux 建议使用无 CGO 构建：

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o singbox-go-tui .
```

也可以执行项目内的 `build-local.sh`。该脚本默认构建 Linux amd64，可以通过
`GOARCH=arm64` 等环境变量切换架构。

## 运行

```bash
./singbox-go-tui
./singbox-go-tui --url http://127.0.0.1:9000
./singbox-go-tui --url http://127.0.0.1:9000 --secret 'your-secret'
```

配置优先级为：默认值、配置文件、环境变量、命令行参数。

```bash
SINGBOX_API_URL=http://127.0.0.1:9000 \
SINGBOX_API_SECRET=your-secret \
./singbox-go-tui
```

`--no-connect` 可以启动后在 Settings 页面手动连接。secret 配置文件会以
`0600` 权限写入。

## 快捷键

```text
1..5       切换页面
Tab        下一页
r          立即刷新
?          帮助
q/Ctrl-C   退出
```

Proxy Groups 页面：Up/Down、Enter、`t`、`x`、`[`、`]`、`m`。

Connections 页面：Up/Down、Enter、`c`、`C`。

Logs 页面：Up/Down、PgUp/PgDn、Home/End、`f`、`a`、`c`。

Settings 页面：Up/Down、Enter/`e`、文字编辑、`c`、`d`。

## 协议实现

客户端直接发送 gRPC-Web protobuf 请求：

```text
Content-Type: application/grpc-web+proto
X-Grpc-Web: 1
Authorization: Bearer <secret>
```

请求和响应使用 1 字节 flags 加 4 字节大端长度的帧格式。服务端流只读取一个
有界快照窗口（状态 1.5 秒，其余流 1 秒），不会因为 sing-box 持续推送而阻塞
终端界面；单次缓冲限制为 16 MiB，最多解码 4096 条消息。

protobuf wire 编解码位于 `wire.go`，因此不需要安装 `protoc` 或生成代码。
