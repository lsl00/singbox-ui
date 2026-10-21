package app

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mihomo-tui/internal/config"
)

// fakeSingBox serves the gRPC-Web API of a sing-box instance.
type fakeSingBox struct {
	server *httptest.Server
}

func newFakeSingBox(t *testing.T) *fakeSingBox {
	t.Helper()
	instance := &fakeSingBox{}
	mux := http.NewServeMux()
	mux.HandleFunc("/daemon.StartedService/GetVersion", func(writer http.ResponseWriter, request *http.Request) {
		writeSingBoxFrame(writer, singBoxVersion("1.14.3"))
	})
	mux.HandleFunc("/daemon.StartedService/SubscribeStatus", func(writer http.ResponseWriter, request *http.Request) {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			return
		}
		// The first sample never carries a rate, the following ones
		// report the difference of the cumulative counters.
		writeSingBoxFrame(writer, singBoxStatus(1024, 9, 3, 2, 0, 0, 100, 200))
		flusher.Flush()
		for i := 0; i < 5; i++ {
			writeSingBoxFrame(writer, singBoxStatus(2048, 11, 4, 3, 500, 1500, 600, 1700))
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	})
	instance.server = httptest.NewServer(mux)
	t.Cleanup(instance.server.Close)
	return instance
}

func writeSingBoxFrame(writer http.ResponseWriter, payload []byte) {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	_, _ = writer.Write(frame)
}

func singBoxVarint(dst []byte, value uint64) []byte {
	for value >= 0x80 {
		dst = append(dst, byte(value)|0x80)
		value >>= 7
	}
	return append(dst, byte(value))
}

func singBoxKey(dst []byte, field, wireType int) []byte {
	return singBoxVarint(dst, uint64(field<<3|wireType))
}

func singBoxInt64(dst []byte, field int, value int64) []byte {
	dst = singBoxKey(dst, field, 0)
	return singBoxVarint(dst, uint64(value))
}

func singBoxVersion(version string) []byte {
	dst := singBoxKey(nil, 1, 2)
	dst = singBoxVarint(dst, uint64(len(version)))
	dst = append(dst, version...)
	return singBoxInt64(dst, 2, 4)
}

func singBoxStatus(memory uint64, goroutines, connectionsIn, connectionsOut int32,
	uplink, downlink, uplinkTotal, downlinkTotal int64) []byte {
	dst := singBoxKey(nil, 1, 0)
	dst = singBoxVarint(dst, memory)
	dst = singBoxInt64(dst, 2, int64(goroutines))
	dst = singBoxInt64(dst, 3, int64(connectionsIn))
	dst = singBoxInt64(dst, 4, int64(connectionsOut))
	dst = singBoxKey(dst, 5, 0)
	dst = singBoxVarint(dst, 1)
	dst = singBoxInt64(dst, 6, uplink)
	dst = singBoxInt64(dst, 7, downlink)
	dst = singBoxInt64(dst, 8, uplinkTotal)
	dst = singBoxInt64(dst, 9, downlinkTotal)
	return dst
}

func TestSingBoxProbeReadsTheStatusStream(t *testing.T) {
	instance := newFakeSingBox(t)
	probe, err := newSingBoxProbe(instance.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := probe.Probe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "1.14.3" {
		t.Fatalf("unexpected version: %+v", info)
	}
	if info.Conns != 7 {
		t.Fatalf("unexpected connection count: %+v", info)
	}
	if info.Goroutines != 11 {
		t.Fatalf("unexpected goroutine count: %+v", info)
	}
	if info.UpTotal != 600 || info.DownTotal != 1700 {
		t.Fatalf("unexpected totals: %+v", info)
	}
	if info.Uplink != 500 || info.Downlink != 1500 {
		t.Fatalf("unexpected rate: %+v", info)
	}
}

func TestServerMonitorUsesTheSingBoxProtocol(t *testing.T) {
	instance := newFakeSingBox(t)
	state := newTestApp()
	state.SetServers([]config.ServerEntry{
		{Name: "alpha", BaseURL: instance.server.URL, Protocol: config.ProtocolSingBox},
	})
	defer state.StopServers()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case event := <-state.Events():
			state.HandleEvent(event)
			if len(state.Servers) > 0 && state.Servers[0].Info != nil {
				monitor := state.Servers[0]
				if !monitor.Connected {
					t.Fatalf("the monitor must be online: %+v", monitor)
				}
				if monitor.Info.Version != "1.14.3" {
					t.Fatalf("unexpected version: %+v", monitor.Info)
				}
				return
			}
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("the monitor did not report any data in time")
}
