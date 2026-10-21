// Package clash talks to the external controller of a mihomo instance over
// its RESTful API and its WebSocket streams.
package clash

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTestURL is the URL mihomo itself uses for health checks.
const DefaultTestURL = "https://www.gstatic.com/generate_204"

// DefaultDelayTimeout is the default timeout of a latency probe.
const DefaultDelayTimeout = 5 * time.Second

// Config describes how to reach a mihomo external controller.
type Config struct {
	URL    string
	Secret string
}

// Client talks to the mihomo external controller over its RESTful API.
//
// The controller is a plain HTTP server, so no HTTP/2 or gRPC machinery is
// needed. Authentication uses the "Authorization: Bearer <secret>" header
// when a secret is configured.
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// New validates cfg and returns a ready to use client.
func New(cfg Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", cfg.URL)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("API URL must use http or https, got %q", parsed.Scheme)
	}

	transport := &http.Transport{
		// The controller usually runs on the loopback interface. Routing
		// it through a system proxy would break local access.
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Client{
		baseURL: baseURL,
		secret:  cfg.Secret,
		http:    &http.Client{Transport: transport},
	}, nil
}

func (c *Client) endpoint(path string) string {
	return c.baseURL + path
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.secret != "" {
		request.Header.Set("Authorization", "Bearer "+c.secret)
	}
	return request, nil
}

// do performs a request and decodes the JSON body into out when out is not nil.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	request, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", method, path, err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, 32*1024*1024))
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %s: %s", method, path, response.Status, strings.TrimSpace(string(data)))
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// Version returns the running mihomo version.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var version Version
	if err := c.do(ctx, http.MethodGet, "/version", nil, &version); err != nil {
		return Version{}, err
	}
	return version, nil
}

// Runtime returns the running configuration.
func (c *Client) Runtime(ctx context.Context) (Runtime, error) {
	var cfg Runtime
	if err := c.do(ctx, http.MethodGet, "/configs", nil, &cfg); err != nil {
		return Runtime{}, err
	}
	return cfg, nil
}

// SetMode switches the tunnel mode (rule, global or direct).
func (c *Client) SetMode(ctx context.Context, mode string) error {
	return c.do(ctx, http.MethodPatch, "/configs", map[string]string{"mode": mode}, nil)
}

// ReloadConfig reloads the configuration file. An empty path makes mihomo
// reuse the configuration file it was started with.
func (c *Client) ReloadConfig(ctx context.Context, path string) error {
	body := map[string]string{"path": path, "payload": ""}
	return c.do(ctx, http.MethodPut, "/configs?force=true", body, nil)
}

// Restart asks the kernel to restart itself. The controller answers before
// the process is replaced, so callers should expect a short outage.
func (c *Client) Restart(ctx context.Context) error {
	body := map[string]string{"path": "", "payload": ""}
	return c.do(ctx, http.MethodPost, "/restart", body, nil)
}

// FlushDNSCache clears the resolver cache.
func (c *Client) FlushDNSCache(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/cache/dns/flush", nil, nil)
}

// FlushFakeIPCache clears the fake-ip mapping cache.
func (c *Client) FlushFakeIPCache(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/cache/fakeip/flush", nil, nil)
}

// Proxies returns every proxy and policy group known to the kernel.
func (c *Client) Proxies(ctx context.Context) (map[string]Proxy, error) {
	var payload struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, &payload); err != nil {
		return nil, err
	}
	if payload.Proxies == nil {
		payload.Proxies = map[string]Proxy{}
	}
	return payload.Proxies, nil
}

// SelectProxy switches the active node of a selector group.
func (c *Client) SelectProxy(ctx context.Context, group, node string) error {
	path := "/proxies/" + url.PathEscape(group)
	return c.do(ctx, http.MethodPut, path, map[string]string{"name": node}, nil)
}

// ProxyDelay measures the latency of a single node.
func (c *Client) ProxyDelay(ctx context.Context, name, testURL string, timeout time.Duration) (uint16, error) {
	path := fmt.Sprintf("/proxies/%s/delay?url=%s&timeout=%d",
		url.PathEscape(name), url.QueryEscape(testURL), timeout.Milliseconds())
	var payload struct {
		Delay uint16 `json:"delay"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return 0, err
	}
	return payload.Delay, nil
}

// GroupDelay measures the latency of every node of a group.
//
// Note that mihomo clears the pinned selection of automatic groups when a
// group wide test runs, so this call is not side effect free.
func (c *Client) GroupDelay(ctx context.Context, name, testURL string, timeout time.Duration) (map[string]uint16, error) {
	path := fmt.Sprintf("/group/%s/delay?url=%s&timeout=%d",
		url.PathEscape(name), url.QueryEscape(testURL), timeout.Milliseconds())
	var payload map[string]uint16
	if err := c.do(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// Connections fetches a one-shot snapshot of the tracked connections.
func (c *Client) Connections(ctx context.Context) (ConnectionsSnapshot, error) {
	var snapshot ConnectionsSnapshot
	if err := c.do(ctx, http.MethodGet, "/connections", nil, &snapshot); err != nil {
		return ConnectionsSnapshot{}, err
	}
	return snapshot, nil
}

// CloseConnection closes a single connection.
func (c *Client) CloseConnection(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/connections/"+url.PathEscape(id), nil, nil)
}

// CloseAllConnections closes every tracked connection.
func (c *Client) CloseAllConnections(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/connections", nil, nil)
}
