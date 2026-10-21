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
		// The first sample carries no rate, the difference to the
		// previous sample is only computed after the first tick. Every
		// following sample reports the difference of the cumulative
		// counters, exactly like sing-box does.
		writeFrame(writer, encodeStatus(1024, 7, 2, 1, true, 0, 0, 100, 200))
		flusher.Flush()
		samples := [][]int64{
			{500, 1500, 600, 1700},
			{1000, 1500, 1600, 3200},
			{0, 1500, 1600, 4700},
		}
		for _, sample := range samples {
			writeFrame(writer, encodeStatus(2048, 9, 3, 2, true,
				sample[0], sample[1], sample[2], sample[3]))
			flusher.Flush()
			// The gap has to be longer than the time the reader needs,
			// otherwise the reader would pick up several samples at once.
			time.Sleep(200 * time.Millisecond)
		}
	})
	instance.server = httptest.NewServer(mux)
	t.Cleanup(instance.server.Close)
	return instance
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
	if version.Version != "1.14.3" || version.APIVersion != 4 {
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

	first, err := stream.Next(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if first.Memory != 1024 || first.Goroutines != 7 {
		t.Fatalf("unexpected status: %+v", first)
	}
	if first.ConnectionsIn != 2 || first.ConnectionsOut != 1 {
		t.Fatalf("unexpected connections: %+v", first)
	}
	if first.UplinkTotal != 100 || first.DownlinkTotal != 200 {
		t.Fatalf("unexpected totals: %+v", first)
	}
	// The first sample of the stream never carries a rate.
	if first.Uplink != 0 || first.Downlink != 0 {
		t.Fatalf("the first sample must not carry a rate: %+v", first)
	}

	second, err := stream.Next(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uplink != 500 || second.Downlink != 1500 {
		t.Fatalf("unexpected rate: %+v", second)
	}
	if second.UplinkTotal != 600 || second.DownlinkTotal != 1700 {
		t.Fatalf("unexpected totals: %+v", second)
	}
	// The rate has to be the difference of the cumulative counters.
	if second.UplinkTotal-first.UplinkTotal != second.Uplink {
		t.Fatalf("the rate does not match the counter delta: %+v", second)
	}
	if second.DownlinkTotal-first.DownlinkTotal != second.Downlink {
		t.Fatalf("the rate does not match the counter delta: %+v", second)
	}
	// The rate is the delta of the interval, so it has to be normalised.
	if second.UplinkRate() != 500 || second.DownlinkRate() != 1500 {
		t.Fatalf("unexpected normalised rate: %+v", second)
	}
	if second.Connections() != 5 {
		t.Fatalf("unexpected connection count: %+v", second)
	}

	third, err := stream.Next(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if third.Uplink != 1000 || third.UplinkTotal != 1600 {
		t.Fatalf("unexpected rate: %+v", third)
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
