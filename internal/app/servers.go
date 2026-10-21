package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
	"mihomo-tui/internal/singbox"
)

// serverStreamTimeout is the deadline of a single read on the event
// streams of a monitored instance.
//
// The connection and the log stream are event driven, so an idle instance
// stays silent until the deadline.
const serverStreamTimeout = 2 * time.Minute

// errNoData reports that a stream had nothing to deliver before the
// deadline. It is not an error of the instance.
var errNoData = errors.New("no data")

// isTimeout reports whether an error means that a stream stayed silent.
func isTimeout(err error) bool {
	return errors.Is(err, singbox.ErrTimeout) ||
		errors.Is(err, os.ErrDeadlineExceeded) ||
		errors.Is(err, context.DeadlineExceeded)
}

// connSource streams the connections of an instance.
type connSource interface {
	// Next blocks until the next batch of changes is available. It
	// reports errNoData when the instance stayed silent.
	Next(ctx context.Context) (connBatch, error)
	// Close terminates the stream.
	Close() error
}

// logSource streams the log records of an instance.
type logSource interface {
	// Next blocks until the next batch of records is available. It
	// reports errNoData when the instance stayed silent.
	Next(ctx context.Context) (logBatch, error)
	// Close terminates the stream.
	Close() error
}

// serverProbe watches one monitored instance.
type serverProbe interface {
	// Probe returns the current state of the instance.
	Probe(ctx context.Context) (ServerInfo, error)
	// OpenConnections subscribes to the connections of the instance.
	OpenConnections(ctx context.Context) (connSource, error)
	// OpenLogs subscribes to the log records of the instance.
	OpenLogs(ctx context.Context) (logSource, error)
	// CloseConnection closes a single connection.
	CloseConnection(ctx context.Context, id string) error
	// CloseAllConnections closes every tracked connection.
	CloseAllConnections(ctx context.Context) error
	// Close releases the resources of the probe.
	Close()
}

// singBoxProbe monitors a sing-box instance over its gRPC-Web API.
//
// sing-box pushes the status samples itself, so the probe keeps the
// subscription open and only reads the next sample from the stream.
type singBoxProbe struct {
	client   *singbox.Client
	version  string
	interval time.Duration

	stream    *singbox.StatusStream
	streamCtx context.Context
	cancel    context.CancelFunc
}

// newSingBoxProbe returns a probe for a sing-box instance.
func newSingBoxProbe(baseURL string) (*singBoxProbe, error) {
	client, err := singbox.New(baseURL, "")
	if err != nil {
		return nil, err
	}
	return &singBoxProbe{client: client, interval: singbox.DefaultStatusInterval}, nil
}

// Probe reads the next status sample of the instance.
func (p *singBoxProbe) Probe(ctx context.Context) (ServerInfo, error) {
	if p.version == "" {
		version, err := p.client.GetVersion(ctx)
		if err != nil {
			return ServerInfo{}, err
		}
		p.version = version.Version
	}
	stream, fresh, err := p.openStream(ctx)
	if err != nil {
		return ServerInfo{}, err
	}
	if fresh {
		// The first sample of a stream never carries a rate, sing-box
		// computes the difference only after the first tick.
		if _, err := stream.Next(5 * p.interval); err != nil {
			p.closeStream()
			return ServerInfo{}, err
		}
	}
	// The stream pushes one sample per interval, so a few missed
	// intervals still mean the instance is alive.
	status, err := stream.Next(5 * p.interval)
	if err != nil {
		p.closeStream()
		return ServerInfo{}, err
	}
	return ServerInfo{
		Version:    p.version,
		Uplink:     status.UplinkRate(),
		Downlink:   status.DownlinkRate(),
		UpTotal:    status.UplinkTotal,
		DownTotal:  status.DownlinkTotal,
		Memory:     status.Memory,
		Conns:      status.Connections(),
		Goroutines: status.Goroutines,
	}, nil
}

// openStream returns the open subscription and opens it when needed.
//
// The second return value reports whether the subscription was just opened.
func (p *singBoxProbe) openStream(ctx context.Context) (*singbox.StatusStream, bool, error) {
	if p.stream != nil {
		return p.stream, false, nil
	}
	// The stream outlives the probe call, so it gets its own context.
	p.streamCtx, p.cancel = context.WithCancel(context.Background())
	stream, err := p.client.OpenStatusStream(p.streamCtx, p.interval)
	if err != nil {
		p.cancel()
		p.streamCtx = nil
		p.cancel = nil
		return nil, false, err
	}
	p.stream = stream
	return stream, true, nil
}

// closeStream drops the subscription so the next probe opens a new one.
func (p *singBoxProbe) closeStream() {
	if p.stream != nil {
		p.stream.Close()
		p.stream = nil
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.streamCtx = nil
}

// OpenConnections subscribes to the connections of the instance.
func (p *singBoxProbe) OpenConnections(ctx context.Context) (connSource, error) {
	stream, err := p.client.OpenConnectionStream(ctx, p.interval)
	if err != nil {
		return nil, err
	}
	return &singBoxConnSource{stream: stream}, nil
}

// OpenLogs subscribes to the log records of the instance.
func (p *singBoxProbe) OpenLogs(ctx context.Context) (logSource, error) {
	stream, err := p.client.OpenLogStream(ctx)
	if err != nil {
		return nil, err
	}
	return &singBoxLogSource{stream: stream}, nil
}

// CloseConnection closes a single connection of the instance.
func (p *singBoxProbe) CloseConnection(ctx context.Context, id string) error {
	return p.client.CloseConnection(ctx, id)
}

// CloseAllConnections closes every tracked connection of the instance.
func (p *singBoxProbe) CloseAllConnections(ctx context.Context) error {
	return p.client.CloseAllConnections(ctx)
}

// Close releases the probe.
func (p *singBoxProbe) Close() {
	p.closeStream()
}

// singBoxConnSource turns the connection events of sing-box into batches
// of changes.
type singBoxConnSource struct {
	stream *singbox.ConnectionStream
}

// Next blocks until the next batch of changes is available.
func (s *singBoxConnSource) Next(ctx context.Context) (connBatch, error) {
	events, err := s.stream.Next(serverStreamTimeout)
	if err != nil {
		if isTimeout(err) {
			return connBatch{}, errNoData
		}
		return connBatch{}, err
	}
	batch := connBatch{Reset: events.Reset}
	for _, event := range events.Events {
		change := connEvent{Kind: int(event.Type), Uploaded: event.UplinkDelta, Downloaded: event.DownlinkDelta}
		if event.Connection != nil {
			change.Info = connFromSingBox(*event.Connection)
		}
		if change.Info.ID == "" {
			change.Info.ID = event.ID
		}
		if event.ClosedAt > 0 {
			change.ClosedAt = time.UnixMilli(event.ClosedAt)
		}
		batch.Events = append(batch.Events, change)
	}
	return batch, nil
}

// Close terminates the stream.
func (s *singBoxConnSource) Close() error {
	return s.stream.Close()
}

// singBoxLogSource turns the log records of sing-box into batches.
type singBoxLogSource struct {
	stream *singbox.LogStream
}

// Next blocks until the next batch of records is available.
func (s *singBoxLogSource) Next(ctx context.Context) (logBatch, error) {
	log, err := s.stream.Next(serverStreamTimeout)
	if err != nil {
		if isTimeout(err) {
			return logBatch{}, errNoData
		}
		return logBatch{}, err
	}
	batch := logBatch{Reset: log.Reset}
	for _, entry := range log.Messages {
		batch.Entries = append(batch.Entries, LogLine{
			Time:  time.Now(),
			Level: singboxLogLevel(entry.Level),
			Text:  entry.Message,
		})
	}
	return batch, nil
}

// Close terminates the stream.
func (s *singBoxLogSource) Close() error {
	return s.stream.Close()
}

// singboxLogLevel maps a level of the sing-box API to the severity names
// the dashboard uses.
//
// The sing-box levels count upwards from panic to trace, the dashboard
// counts upwards from debug to error, so the mapping is not a rename.
func singboxLogLevel(level int32) string {
	switch singbox.LogLevelName(level) {
	case "panic", "fatal", "error":
		return "error"
	case "warn":
		return "warning"
	case "info":
		return "info"
	default:
		return "debug"
	}
}

// clashProbe monitors a mihomo instance over its RESTful API.
//
// The RESTful API has no status stream, so the probe polls the connection
// snapshot and derives the transfer rate from the difference between two
// polls.
type clashProbe struct {
	client  *clash.Client
	version string

	lastUp    int64
	lastDown  int64
	lastTime  time.Time
	hasSample bool
}

// newClashProbe returns a probe for a mihomo instance.
func newClashProbe(baseURL string) (*clashProbe, error) {
	client, err := clash.New(clash.Config{URL: baseURL})
	if err != nil {
		return nil, err
	}
	return &clashProbe{client: client}, nil
}

// Probe reads the current state of the instance.
func (p *clashProbe) Probe(ctx context.Context) (ServerInfo, error) {
	version, err := p.client.Version(ctx)
	if err != nil {
		return ServerInfo{}, err
	}
	p.version = version.Version

	snapshot, err := p.client.Connections(ctx)
	if err != nil {
		return ServerInfo{}, err
	}
	info := ServerInfo{
		Version:   p.version,
		UpTotal:   snapshot.UploadTotal,
		DownTotal: snapshot.DownloadTotal,
		Memory:    snapshot.Memory,
		Conns:     len(snapshot.Connections),
	}
	now := time.Now()
	if p.hasSample && now.After(p.lastTime) {
		elapsed := now.Sub(p.lastTime).Seconds()
		if elapsed > 0 {
			info.Uplink = int64(float64(info.UpTotal-p.lastUp) / elapsed)
			info.Downlink = int64(float64(info.DownTotal-p.lastDown) / elapsed)
			if info.Uplink < 0 {
				info.Uplink = 0
			}
			if info.Downlink < 0 {
				info.Downlink = 0
			}
		}
	}
	p.lastUp = info.UpTotal
	p.lastDown = info.DownTotal
	p.lastTime = now
	p.hasSample = true
	return info, nil
}

// OpenConnections subscribes to the connections of the instance.
//
// The mihomo API pushes a full snapshot every second, so the source
// reports every connection as new.
func (p *clashProbe) OpenConnections(ctx context.Context) (connSource, error) {
	stream, err := p.client.DialStream(ctx, "/connections", nil)
	if err != nil {
		return nil, err
	}
	return &clashConnSource{stream: stream}, nil
}

// OpenLogs subscribes to the log records of the instance.
func (p *clashProbe) OpenLogs(ctx context.Context) (logSource, error) {
	stream, err := p.client.DialStream(ctx, "/logs", nil)
	if err != nil {
		return nil, err
	}
	return &clashLogSource{stream: stream}, nil
}

// CloseConnection closes a single connection of the instance.
func (p *clashProbe) CloseConnection(ctx context.Context, id string) error {
	return p.client.CloseConnection(ctx, id)
}

// CloseAllConnections closes every tracked connection of the instance.
func (p *clashProbe) CloseAllConnections(ctx context.Context) error {
	return p.client.CloseAllConnections(ctx)
}

// Close releases the probe.
func (p *clashProbe) Close() {}

// clashConnSource turns the connection snapshots of mihomo into batches.
type clashConnSource struct {
	stream *clash.Stream
}

// Next blocks until the next snapshot is available.
func (s *clashConnSource) Next(ctx context.Context) (connBatch, error) {
	var snapshot clash.ConnectionsSnapshot
	if err := s.stream.ReadJSON(ctx, &snapshot); err != nil {
		return connBatch{}, err
	}
	batch := connBatch{Reset: true}
	for _, connection := range snapshot.Connections {
		batch.Events = append(batch.Events, connEvent{
			Kind:       connNew,
			Info:       connFromClash(connection),
			Uploaded:   connection.Upload,
			Downloaded: connection.Download,
		})
	}
	return batch, nil
}

// Close terminates the stream.
func (s *clashConnSource) Close() error {
	return s.stream.Close()
}

// clashLogSource turns the log records of mihomo into batches.
type clashLogSource struct {
	stream *clash.Stream
}

// Next blocks until the next record is available.
func (s *clashLogSource) Next(ctx context.Context) (logBatch, error) {
	var entry clash.LogEntry
	if err := s.stream.ReadJSON(ctx, &entry); err != nil {
		if isTimeout(err) {
			return logBatch{}, errNoData
		}
		return logBatch{}, err
	}
	return logBatch{Entries: []LogLine{{
		Time:  time.Now(),
		Level: strings.ToLower(entry.Type),
		Text:  entry.Payload,
	}}}, nil
}

// Close terminates the stream.
func (s *clashLogSource) Close() error {
	return s.stream.Close()
}

// ServerMonitor tracks one additional instance.
type ServerMonitor struct {
	Entry config.ServerEntry
	Probe serverProbe

	Connected bool
	Err       string
	Busy      bool

	Info *ServerInfo
}

// SetServers replaces the list of monitored instances.
//
// Every instance gets its own probe. A sing-box instance keeps a status
// subscription open, a mihomo instance is polled.
func (a *App) SetServers(entries []config.ServerEntry) {
	a.StopServers()
	a.Servers = a.Servers[:0]
	for _, entry := range entries {
		monitor := ServerMonitor{Entry: entry}
		var probe serverProbe
		var err error
		if entry.Protocol == config.ProtocolClash {
			probe, err = newClashProbe(entry.BaseURL)
		} else {
			probe, err = newSingBoxProbe(entry.BaseURL)
		}
		if err != nil {
			monitor.Err = err.Error()
		} else {
			monitor.Probe = probe
		}
		a.Servers = append(a.Servers, monitor)
	}
	a.ServerCursor = 0
	a.ServerScroll = 0
	a.RefreshServers()
}

// StopServers closes the probes of the monitored instances.
func (a *App) StopServers() {
	for index := range a.Servers {
		if a.Servers[index].Probe != nil {
			a.Servers[index].Probe.Close()
		}
	}
}

// RefreshServers polls every monitored instance.
func (a *App) RefreshServers() {
	for index := range a.Servers {
		monitor := &a.Servers[index]
		if monitor.Busy || monitor.Probe == nil {
			continue
		}
		monitor.Busy = true
		probe := monitor.Probe
		go func(index int) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			info, err := probe.Probe(ctx)
			a.events <- Event{Kind: EventServer, ServerIndex: index, Err: err, ServerInfo: &info}
		}(index)
	}
}

// handleServer applies the result of a monitored instance poll.
func (a *App) handleServer(event Event) {
	if event.ServerIndex < 0 || event.ServerIndex >= len(a.Servers) {
		return
	}
	monitor := &a.Servers[event.ServerIndex]
	monitor.Busy = false
	if event.Err != nil {
		monitor.Connected = false
		monitor.Err = event.Err.Error()
		return
	}
	monitor.Connected = true
	monitor.Err = ""
	if event.ServerInfo != nil {
		monitor.Info = event.ServerInfo
	}
}

// OpenServer opens the details of a monitored instance.
//
// The connections and the log records of the instance replace the ones of
// the local kernel until the instance is closed again.
func (a *App) OpenServer(index int) {
	if index < 0 || index >= len(a.Servers) {
		return
	}
	if a.OpenedServer == index {
		a.CloseServer()
		return
	}
	monitor := &a.Servers[index]
	if monitor.Probe == nil {
		a.Message = fmt.Sprintf("%s is not reachable", monitor.Entry.Name)
		return
	}
	a.CloseServer()
	a.OpenedServer = index
	a.serverConns.reset()
	a.serverLogs = nil
	a.serverConnCursor = 0
	a.serverLogOff = 0
	a.serverLogFoll = true
	a.Message = "opened " + monitor.Entry.Name

	ctx, cancel := context.WithCancel(context.Background())
	a.serverCancel = cancel
	go a.superviseServerConnections(ctx, index)
	go a.superviseServerLogs(ctx, index)
}

// CloseServer closes the details of the opened instance.
func (a *App) CloseServer() {
	if a.serverCancel != nil {
		a.serverCancel()
		a.serverCancel = nil
	}
	a.OpenedServer = -1
	a.serverConns.reset()
	a.serverLogs = nil
	a.serverConnCursor = 0
	a.serverLogOff = 0
}

// superviseServerConnections keeps the connection stream of an instance
// open and forwards the changes to the dashboard.
//
// The stream is event driven, so an idle instance stays silent. The stream
// is opened again after every timeout, which also resynchronises the
// connection state.
func (a *App) superviseServerConnections(ctx context.Context, index int) {
	for ctx.Err() == nil {
		monitor := &a.Servers[index]
		source, err := monitor.Probe.OpenConnections(ctx)
		if err != nil {
			a.events <- Event{Kind: EventServerError, ServerIndex: index, Err: err}
			if !sleepContext(ctx, 2*time.Second) {
				return
			}
			continue
		}
		err = a.pumpServerConnections(ctx, index, source)
		source.Close()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.events <- Event{Kind: EventServerError, ServerIndex: index, Err: err}
			if !sleepContext(ctx, 2*time.Second) {
				return
			}
		}
	}
}

// pumpServerConnections forwards the connection changes of an instance.
func (a *App) pumpServerConnections(ctx context.Context, index int, source connSource) error {
	for ctx.Err() == nil {
		batch, err := source.Next(ctx)
		if err != nil {
			if errors.Is(err, errNoData) {
				// The instance is idle, the stream is opened again.
				return nil
			}
			return err
		}
		a.events <- Event{Kind: EventServerConnections, ServerIndex: index, ConnBatch: &batch}
	}
	return nil
}

// superviseServerLogs keeps the log stream of an instance open and
// forwards the records to the dashboard.
func (a *App) superviseServerLogs(ctx context.Context, index int) {
	for ctx.Err() == nil {
		monitor := &a.Servers[index]
		source, err := monitor.Probe.OpenLogs(ctx)
		if err != nil {
			a.events <- Event{Kind: EventServerError, ServerIndex: index, Err: err}
			if !sleepContext(ctx, 2*time.Second) {
				return
			}
			continue
		}
		err = a.pumpServerLogs(ctx, index, source)
		source.Close()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.events <- Event{Kind: EventServerError, ServerIndex: index, Err: err}
			if !sleepContext(ctx, 2*time.Second) {
				return
			}
		}
	}
}

// pumpServerLogs forwards the log records of an instance.
func (a *App) pumpServerLogs(ctx context.Context, index int, source logSource) error {
	for ctx.Err() == nil {
		batch, err := source.Next(ctx)
		if err != nil {
			if errors.Is(err, errNoData) {
				return nil
			}
			return err
		}
		a.events <- Event{Kind: EventServerLog, ServerIndex: index, LogBatch: &batch}
	}
	return nil
}

// sleepContext waits for the duration and reports whether the context
// survived.
func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// handleServerEvent applies an event of the opened instance.
//
// The events of an instance carry the index of the instance, so the
// events of a previously opened instance are ignored.
func (a *App) handleServerEvent(event Event) {
	if event.ServerIndex != a.OpenedServer {
		return
	}
	switch event.Kind {
	case EventServerConnections:
		if event.ConnBatch == nil {
			return
		}
		a.applyConnBatch(*event.ConnBatch)
	case EventServerLog:
		if event.LogBatch == nil {
			return
		}
		a.applyLogBatch(*event.LogBatch)
	case EventServerError:
		if event.Err != nil {
			a.Error = event.Err.Error()
		}
	}
}

// OverlayTitle returns the title of the overlay of the opened instance.
func (a *App) OverlayTitle() string {
	if a.OpenedServer < 0 || a.OpenedServer >= len(a.Servers) {
		return ""
	}
	monitor := a.Servers[a.OpenedServer]
	title := monitor.Entry.Name
	if monitor.Info != nil && monitor.Info.Version != "" {
		title += "  " + monitor.Info.Version
	}
	return title + "   [Esc closes]"
}

// OverlayCursor returns the cursor of the connection list of the overlay.
func (a *App) OverlayCursor() int {
	return a.serverConnCursor
}

// ServerLogFollow reports whether the log records of the overlay follow
// the newest record.
func (a *App) ServerLogFollow() bool {
	return a.serverLogFoll
}

// ServerLogOffset returns the scroll offset of the log records of the
// overlay.
func (a *App) ServerLogOffset() int {
	return a.serverLogOff
}
