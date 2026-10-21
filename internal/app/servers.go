package app

import (
	"context"
	"time"

	"mihomo-tui/internal/clash"
	"mihomo-tui/internal/config"
	"mihomo-tui/internal/singbox"
)

// serverProbe collects the state of one monitored instance.
type serverProbe interface {
	// Probe returns the current state of the instance.
	Probe(ctx context.Context) (ServerInfo, error)
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

// Close releases the probe.
func (p *singBoxProbe) Close() {
	p.closeStream()
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

// Close releases the probe.
func (p *clashProbe) Close() {}

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
