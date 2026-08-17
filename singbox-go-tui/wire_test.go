package main

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFrameParserAcceptsChunkedFramesAndTrailers(t *testing.T) {
	dataFrame, err := encodeFrame([]byte("status"))
	if err != nil {
		t.Fatal(err)
	}
	trailer := []byte("grpc-status: 0\r\n")
	trailerFrame := make([]byte, 5+len(trailer))
	trailerFrame[0] = 0x80
	binary.BigEndian.PutUint32(trailerFrame[1:5], uint32(len(trailer)))
	copy(trailerFrame[5:], trailer)
	stream := append(dataFrame, trailerFrame...)

	parser := frameParser{}
	var frames [][]byte
	for _, value := range stream {
		chunk, feedErr := parser.feed([]byte{value})
		if feedErr != nil {
			t.Fatal(feedErr)
		}
		frames = append(frames, chunk...)
	}
	if err := parser.finish(); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || string(frames[0]) != "status" {
		t.Fatalf("unexpected frames: %#v", frames)
	}
}

func TestFrameParserRejectsNonZeroGRPCStatus(t *testing.T) {
	trailer := []byte("grpc-status: 13\r\ngrpc-message: unavailable%20now\r\n")
	frame := make([]byte, 5+len(trailer))
	frame[0] = 0x80
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(trailer)))
	copy(frame[5:], trailer)
	_, err := decodeFrames(frame)
	if err == nil || err.Error() != "gRPC error code 13: unavailable now" {
		t.Fatalf("unexpected trailer error: %v", err)
	}
}

func TestDecodeStatus(t *testing.T) {
	body := appendUint64Field(nil, 1, 1024)
	body = appendInt64(body, 2, 17)
	body = appendInt64(body, 6, 2048)
	body = appendInt64(body, 7, 4096)
	body = appendInt64(body, 8, 8192)
	body = appendInt64(body, 9, 16384)
	body = appendBool(body, 5, true)

	status, err := decodeStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	if status.Memory != 1024 || status.Goroutines != 17 || !status.TrafficAvailable || status.Uplink != 2048 || status.DownlinkTotal != 16384 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestDecodeGroupsAndConnectionEvents(t *testing.T) {
	item := appendString(nil, 1, "node-a")
	item = appendString(item, 2, "shadowsocks")
	item = appendInt64(item, 4, 42)
	group := appendString(nil, 1, "proxy")
	group = appendString(group, 2, "selector")
	group = appendBool(group, 3, true)
	group = appendString(group, 4, "node-a")
	group = appendBool(group, 5, true)
	group = appendMessage(group, 6, item)
	groupsBody := appendMessage(nil, 1, group)

	groups, err := decodeGroups(groupsBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Tag != "proxy" || len(groups[0].Items) != 1 || groups[0].Items[0].URLTestDelay != 42 {
		t.Fatalf("unexpected groups: %+v", groups)
	}

	connection := appendString(nil, 1, "conn-1")
	connection = appendString(connection, 8, "example.com")
	connection = appendString(connection, 9, "tcp")
	event := appendInt64(nil, 1, 0)
	event = appendString(event, 2, "conn-1")
	event = appendMessage(event, 3, connection)
	eventsBody := appendMessage(nil, 1, event)
	events, err := decodeConnectionEvents(eventsBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 || events.Events[0].Connection == nil || events.Events[0].Connection.Domain != "example.com" {
		t.Fatalf("unexpected connection events: %+v", events)
	}
}

func TestAPIClientUnaryRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/daemon.StartedService/GetVersion" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Content-Type") != "application/grpc-web+proto" || request.Header.Get("X-Grpc-Web") != "1" {
			t.Fatalf("unexpected headers: %#v", request.Header)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("missing authorization header")
		}
		body := appendString(nil, 1, "1.14.0")
		body = appendInt64(body, 2, 1)
		frame, err := encodeFrame(body)
		if err != nil {
			t.Fatal(err)
		}
		writer.Header().Set("Content-Type", "application/grpc-web+proto")
		_, _ = writer.Write(frame)
	}))
	defer server.Close()

	client, err := newAPIClient(APIConfig{URL: server.URL, Secret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := client.getVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != "1.14.0" || version.APIVersion != 1 {
		t.Fatalf("unexpected version: %+v", version)
	}
}

func TestAPIClientBoundsServerStreamRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body := appendInt64(nil, 2, 3)
		body = appendInt64(body, 6, 128)
		frame, err := encodeFrame(body)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = writer.Write(frame)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	client, err := newAPIClient(APIConfig{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	values, err := client.subscribeStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Goroutines != 3 || values[0].Uplink != 128 {
		t.Fatalf("unexpected stream values: %+v", values)
	}
	if elapsed := time.Since(started); elapsed < time.Second || elapsed > 3*time.Second {
		t.Fatalf("stream was not bounded as expected: %s", elapsed)
	}
}

func appendUint64Field(dst []byte, field int, value uint64) []byte {
	dst = appendKey(dst, field, wireVarint)
	return appendVarint(dst, value)
}

func TestEncodeFrameHeader(t *testing.T) {
	frame, err := encodeFrame([]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 0, 3, 1, 2, 3}
	if !bytes.Equal(frame, want) {
		t.Fatalf("frame = %v, want %v", frame, want)
	}
}
