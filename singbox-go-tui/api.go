package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const grpcService = "daemon.StartedService"

type APIClient struct {
	httpClient *http.Client
	baseURL    string
	secret     string
}

func newAPIClient(config APIConfig) (*APIClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.URL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", config.URL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("API URL must use http or https, got %q", parsed.Scheme)
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
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
	return &APIClient{
		httpClient: &http.Client{Transport: transport},
		baseURL:    baseURL,
		secret:     config.Secret,
	}, nil
}

func (c *APIClient) methodURL(method string) string {
	return c.baseURL + "/" + grpcService + "/" + method
}

func (c *APIClient) do(ctx context.Context, method string, frame []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL(method), bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/grpc-web+proto")
	request.Header.Set("X-Grpc-Web", "1")
	if c.secret != "" {
		request.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return c.httpClient.Do(request)
}

func (c *APIClient) unary(method string, frame []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := c.do(ctx, method, frame)
	if err != nil {
		return nil, fmt.Errorf("gRPC request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP error: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxStreamBuffer+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if len(data) > maxStreamBuffer {
		return nil, errors.New("gRPC response exceeded 16 MiB")
	}
	frames, err := decodeFrames(data)
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, nil
	}
	return frames[0], nil
}

func (c *APIClient) serverStream(method string, frame []byte, duration time.Duration) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	response, err := c.do(ctx, method, frame)
	if err != nil {
		return nil, fmt.Errorf("gRPC request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP error: %s", response.Status)
	}

	parser := frameParser{}
	messages := make([][]byte, 0)
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			frames, parseErr := parser.feed(buffer[:count])
			if parseErr != nil {
				return nil, parseErr
			}
			messages = append(messages, frames...)
			if len(messages) > maxStreamMessages {
				return nil, errors.New("gRPC stream contained too many messages")
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return messages, nil
		}
		// A stream is deliberately read for a bounded total interval. The
		// context ending after at least one response is normal for sing-box.
		if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return messages, nil
		}
		return nil, fmt.Errorf("gRPC stream error: %w", readErr)
	}
}

func (c *APIClient) getVersion() (VersionInfo, error) {
	frame, err := encodeFrame(nil)
	if err != nil {
		return VersionInfo{}, err
	}
	data, err := c.unary("GetVersion", frame)
	if err != nil {
		return VersionInfo{}, err
	}
	if len(data) == 0 {
		return VersionInfo{}, errors.New("empty GetVersion response")
	}
	return decodeVersion(data)
}

func (c *APIClient) subscribeStatus() ([]StatusInfo, error) {
	frame, err := encodeStatusRequest()
	if err != nil {
		return nil, err
	}
	frames, err := c.serverStream("SubscribeStatus", frame, 1500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	values := make([]StatusInfo, 0, len(frames))
	for _, frame := range frames {
		value, decodeErr := decodeStatus(frame)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode status: %w", decodeErr)
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *APIClient) subscribeServiceStatus() ([]ServiceStatusInfo, error) {
	frame, err := encodeFrame(nil)
	if err != nil {
		return nil, err
	}
	frames, err := c.serverStream("SubscribeServiceStatus", frame, time.Second)
	if err != nil {
		return nil, err
	}
	values := make([]ServiceStatusInfo, 0, len(frames))
	for _, frame := range frames {
		value, decodeErr := decodeServiceStatus(frame)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode service status: %w", decodeErr)
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *APIClient) subscribeGroups() ([]GroupInfo, error) {
	frame, err := encodeFrame(nil)
	if err != nil {
		return nil, err
	}
	frames, err := c.serverStream("SubscribeGroups", frame, time.Second)
	if err != nil {
		return nil, err
	}
	groups := make([]GroupInfo, 0)
	for _, frame := range frames {
		values, decodeErr := decodeGroups(frame)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode groups: %w", decodeErr)
		}
		groups = append(groups, values...)
	}
	return groups, nil
}

func (c *APIClient) subscribeConnections() ([]ConnectionEventsInfo, error) {
	frame, err := encodeConnectionsRequest()
	if err != nil {
		return nil, err
	}
	frames, err := c.serverStream("SubscribeConnections", frame, time.Second)
	if err != nil {
		return nil, err
	}
	values := make([]ConnectionEventsInfo, 0, len(frames))
	for _, frame := range frames {
		value, decodeErr := decodeConnectionEvents(frame)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode connections: %w", decodeErr)
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *APIClient) subscribeLogs() ([]LogsInfo, error) {
	frame, err := encodeFrame(nil)
	if err != nil {
		return nil, err
	}
	frames, err := c.serverStream("SubscribeLog", frame, time.Second)
	if err != nil {
		return nil, err
	}
	values := make([]LogsInfo, 0, len(frames))
	for _, frame := range frames {
		value, decodeErr := decodeLog(frame)
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to decode logs: %w", decodeErr)
		}
		values = append(values, value)
	}
	return values, nil
}

func (c *APIClient) getStartedAt() (int64, error) {
	frame, err := encodeFrame(nil)
	if err != nil {
		return 0, err
	}
	data, err := c.unary("GetStartedAt", frame)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, errors.New("empty GetStartedAt response")
	}
	return decodeStartedAt(data)
}

func (c *APIClient) getClashModeStatus() (ClashModeInfo, error) {
	frame, err := encodeFrame(nil)
	if err != nil {
		return ClashModeInfo{}, err
	}
	data, err := c.unary("GetClashModeStatus", frame)
	if err != nil {
		return ClashModeInfo{}, err
	}
	if len(data) == 0 {
		return ClashModeInfo{}, errors.New("empty GetClashModeStatus response")
	}
	return decodeClashMode(data)
}

func (c *APIClient) clearLogs() error {
	frame, err := encodeFrame(nil)
	if err != nil {
		return err
	}
	_, err = c.unary("ClearLogs", frame)
	return err
}

func (c *APIClient) selectOutbound(group, outbound string) error {
	frame, err := encodeSelectOutboundRequest(group, outbound)
	if err != nil {
		return err
	}
	_, err = c.unary("SelectOutbound", frame)
	return err
}

func (c *APIClient) urlTest(outbound string) error {
	frame, err := encodeURLTestRequest(outbound)
	if err != nil {
		return err
	}
	_, err = c.unary("URLTest", frame)
	return err
}

func (c *APIClient) setGroupExpand(group string, expanded bool) error {
	frame, err := encodeSetGroupExpandRequest(group, expanded)
	if err != nil {
		return err
	}
	_, err = c.unary("SetGroupExpand", frame)
	return err
}

func (c *APIClient) setClashMode(mode string) error {
	frame, err := encodeClashModeRequest(mode)
	if err != nil {
		return err
	}
	_, err = c.unary("SetClashMode", frame)
	return err
}

func (c *APIClient) closeConnection(id string) error {
	frame, err := encodeCloseConnectionRequest(id)
	if err != nil {
		return err
	}
	_, err = c.unary("CloseConnection", frame)
	return err
}

func (c *APIClient) closeAllConnections() error {
	frame, err := encodeFrame(nil)
	if err != nil {
		return err
	}
	_, err = c.unary("CloseAllConnections", frame)
	return err
}
