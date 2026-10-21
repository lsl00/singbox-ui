// Package singbox talks to the gRPC-Web API of a sing-box instance. The
// protobuf codec is implemented by hand, so no protoc and no generated
// code are needed.
package singbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Service is the name of the gRPC service exposed by sing-box.
const Service = "daemon.StartedService"

// DefaultStatusInterval is the push interval of the status stream.
const DefaultStatusInterval = time.Second

// Version is the payload of the GetVersion call.
type Version struct {
	Version string
}

// Status is the payload of the SubscribeStatus stream.
//
// Uplink and Downlink hold the number of bytes transferred during the last
// push interval, not a normalised rate. Use UplinkRate and DownlinkRate to
// get bytes per second.
type Status struct {
	Memory         uint64
	Goroutines     int32
	ConnectionsIn  int32
	ConnectionsOut int32
	Uplink         int64
	Downlink       int64
	UplinkTotal    int64
	DownlinkTotal  int64

	// Interval is the push interval the rates refer to.
	Interval time.Duration
}

// UplinkRate returns the upload rate in bytes per second.
func (s Status) UplinkRate() int64 {
	return s.rate(s.Uplink)
}

// DownlinkRate returns the download rate in bytes per second.
func (s Status) DownlinkRate() int64 {
	return s.rate(s.Downlink)
}

func (s Status) rate(value int64) int64 {
	if value <= 0 {
		return 0
	}
	if s.Interval <= 0 {
		return value
	}
	return int64(float64(value) / s.Interval.Seconds())
}

// Connections returns the number of tracked connections.
func (s Status) Connections() int {
	return int(s.ConnectionsIn + s.ConnectionsOut)
}

// Client talks to the gRPC-Web API of a sing-box instance.
type Client struct {
	httpClient *http.Client
	baseURL    string
	secret     string
}

// New validates the address and returns a ready to use client.
func New(baseURL, secret string) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", baseURL)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("API URL must use http or https, got %q", parsed.Scheme)
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// The instance usually runs on a private address, so it must not
		// be routed through a system proxy.
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Client{
		httpClient: &http.Client{Transport: transport},
		baseURL:    baseURL,
		secret:     secret,
	}, nil
}

func (c *Client) methodURL(method string) string {
	return c.baseURL + "/" + Service + "/" + method
}

// request builds a gRPC-Web request for the given method.
func (c *Client) request(ctx context.Context, method string, body []byte) (*http.Request, error) {
	frame := make([]byte, 5+len(body))
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(body)))
	copy(frame[5:], body)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(method), bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/grpc-web+proto")
	request.Header.Set("X-Grpc-Web", "1")
	if c.secret != "" {
		request.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return request, nil
}

// GetVersion returns the version of the running sing-box instance.
func (c *Client) GetVersion(ctx context.Context) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	request, err := c.request(ctx, "GetVersion", nil)
	if err != nil {
		return Version{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Version{}, fmt.Errorf("gRPC request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Version{}, fmt.Errorf("HTTP error: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxFrameSize))
	if err != nil {
		return Version{}, fmt.Errorf("failed to read response: %w", err)
	}
	parser := frameParser{}
	if err := parser.feed(data); err != nil {
		return Version{}, err
	}
	frames := parser.frames()
	if len(frames) == 0 {
		return Version{}, errors.New("empty GetVersion response")
	}
	return decodeVersion(frames[0])
}
