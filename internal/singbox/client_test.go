package singbox

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeInstance serves the gRPC-Web API of a sing-box instance.
type fakeInstance struct {
	server *httptest.Server
}

func newFakeInstance(t *testing.T) *fakeInstance {
	t.Helper()
	instance := &fakeInstance{}
	mux := http.NewServeMux()
	mux.HandleFunc("/daemon.StartedService/GetVersion", func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Content-Type") != "application/grpc-web+proto" ||
			request.Header.Get("X-Grpc-Web") != "1" {
			t.Errorf("unexpected headers: %#v", request.Header)
		}
		payload := appendString(nil, 1, "1.14.3")
		payload = appendInt64(payload, 2, 4)
		writeFrame(writer, payload)
	})
	mux.HandleFunc("/daemon.StartedService/SubscribeStatus", func(writer http.ResponseWriter, request *http.Request) {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Error("the test server must support flushing")
			return
		}
		// The samples repeat forever, so a reader that skips one of
		// them still gets the next one. The goroutine count carries the
		// index of the sample, which lets the test map a received sample
		// back to its expected values.
		for index := 0; ; index++ {
			select {
			case <-request.Context().Done():
				return
			default:
			}
			sample := statusSamples[index%len(statusSamples)]
			writeFrame(writer, encodeStatus(sample.memory, int32(100+index), 3, 2, true,
				sample.uplink, sample.downlink, sample.upTotal, sample.downTotal))
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	})
	instance.server = httptest.NewServer(mux)
	t.Cleanup(instance.server.Close)
	return instance
}

// statusSample is one expected sample of the fake instance.
type statusSample struct {
	memory             uint64
	uplink, downlink   int64
	upTotal, downTotal int64
}

// statusSamples are the samples the fake instance sends in a loop.
//
// The first one carries no rate, because sing-box computes the difference
// to the previous sample only after the first tick. Every following
// sample reports the difference of the cumulative counters.
var statusSamples = []statusSample{
	{memory: 1024, uplink: 0, downlink: 0, upTotal: 100, downTotal: 200},
	{memory: 2048, uplink: 500, downlink: 1500, upTotal: 600, downTotal: 1700},
	{memory: 2048, uplink: 1000, downlink: 1500, upTotal: 1600, downTotal: 3200},
	{memory: 2048, uplink: 0, downlink: 1500, upTotal: 1600, downTotal: 4700},
}

func writeFrame(writer http.ResponseWriter, payload []byte) {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	_, _ = writer.Write(frame)
}

func appendString(dst []byte, field int, value string) []byte {
	dst = appendKey(dst, field, wireBytes)
	dst = appendVarint(dst, uint64(len(value)))
	return append(dst, value...)
}

// encodeStatus builds a Status message.
func encodeStatus(memory uint64, goroutines, connectionsIn, connectionsOut int32,
	traffic bool, uplink, downlink, uplinkTotal, downlinkTotal int64) []byte {
	dst := appendKey(nil, 1, wireVarint)
	dst = appendVarint(dst, memory)
	dst = appendInt64(dst, 2, int64(goroutines))
	dst = appendInt64(dst, 3, int64(connectionsIn))
	dst = appendInt64(dst, 4, int64(connectionsOut))
	if traffic {
		dst = appendKey(dst, 5, wireVarint)
		dst = appendVarint(dst, 1)
	}
	dst = appendInt64(dst, 6, uplink)
	dst = appendInt64(dst, 7, downlink)
	dst = appendInt64(dst, 8, uplinkTotal)
	dst = appendInt64(dst, 9, downlinkTotal)
	return dst
}

func TestClientGetVersion(t *testing.T) {
	instance := newFakeInstance(t)
	client, err := New(instance.server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.GetVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != "1.14.3" {
		t.Fatalf("unexpected version: %+v", version)
	}
}

func TestClientRejectsInvalidURL(t *testing.T) {
	for _, raw := range []string{"", "localhost:9000", "ftp://localhost:9000"} {
		if _, err := New(raw, ""); err == nil {
			t.Fatalf("expected an error for %q", raw)
		}
	}
}

func TestStatusStreamDecodesSamples(t *testing.T) {
	instance := newFakeInstance(t)
	client, err := New(instance.server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.OpenStatusStream(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// The reader may skip samples, so every received sample is checked
	// against the table of the fake instance.
	for i := 0; i < 6; i++ {
		status, err := stream.Next(5 * time.Second)
		if err != nil {
			t.Fatal(err)
		}
		index := int(status.Goroutines) - 100
		want := statusSamples[index%len(statusSamples)]
		if status.Memory != want.memory {
			t.Fatalf("unexpected memory: %+v", status)
		}
		if status.ConnectionsIn != 3 || status.ConnectionsOut != 2 {
			t.Fatalf("unexpected connections: %+v", status)
		}
		if status.Uplink != want.uplink || status.Downlink != want.downlink {
			t.Fatalf("unexpected rate: %+v", status)
		}
		if status.UplinkTotal != want.upTotal || status.DownlinkTotal != want.downTotal {
			t.Fatalf("unexpected totals: %+v", status)
		}
		// The rate is the difference of the cumulative counters, the
		// interval is one second, so the rate is the delta itself.
		if status.UplinkRate() != want.uplink || status.DownlinkRate() != want.downlink {
			t.Fatalf("unexpected normalised rate: %+v", status)
		}
		if status.Connections() != 5 {
			t.Fatalf("unexpected connection count: %+v", status)
		}
	}
}

func TestStatusRateNormalisation(t *testing.T) {
	status := Status{Uplink: 1500, Downlink: 3000, Interval: 3 * time.Second}
	if status.UplinkRate() != 500 || status.DownlinkRate() != 1000 {
		t.Fatalf("unexpected rate: %+v", status)
	}
	// Without an interval the delta is taken as the rate.
	status.Interval = 0
	if status.UplinkRate() != 1500 {
		t.Fatalf("unexpected rate: %+v", status)
	}
}

func TestFrameParserHandlesChunkedInput(t *testing.T) {
	first := encodeStatus(1, 2, 3, 4, true, 5, 6, 7, 8)
	second := encodeStatus(9, 10, 11, 12, true, 13, 14, 15, 16)

	var payload []byte
	payload = append(payload, frameBytes(first)...)
	payload = append(payload, frameBytes(second)...)

	parser := frameParser{}
	// Feed the payload byte by byte to exercise the partial frame path.
	for index := 0; index < len(payload); index++ {
		if err := parser.feed(payload[index : index+1]); err != nil {
			t.Fatal(err)
		}
	}
	frames := parser.frames()
	if len(frames) != 2 {
		t.Fatalf("expected two frames, got %d", len(frames))
	}
	status, err := decodeStatus(frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if status.Memory != 1 || status.Goroutines != 2 {
		t.Fatalf("unexpected status: %+v", status)
	}
	status, err = decodeStatus(frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if status.Memory != 9 || status.Goroutines != 10 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestFrameParserRejectsFailedStatus(t *testing.T) {
	trailer := []byte("grpc-status: 13\r\ngrpc-message: unavailable%20now\r\n")
	frame := make([]byte, 5+len(trailer))
	frame[0] = 0x80
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(trailer)))
	copy(frame[5:], trailer)

	parser := frameParser{}
	err := parser.feed(frame)
	if err == nil || err.Error() != "gRPC error code 13: unavailable now" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetVersion(context.Background()); err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
}

func TestStatusStreamTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		flusher, ok := writer.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		<-request.Context().Done()
	}))
	defer server.Close()
	client, err := New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.OpenStatusStream(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(300 * time.Millisecond); err == nil {
		t.Fatal("expected a timeout")
	}
}

func frameBytes(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

var _ = io.EOF
