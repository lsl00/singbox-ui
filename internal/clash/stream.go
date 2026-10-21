package clash

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Dialer settings shared by every stream.
var (
	// HandshakeTimeout bounds the websocket upgrade.
	HandshakeTimeout = 10 * time.Second
	// ReadTimeout bounds a single read from a stream. mihomo pushes
	// traffic and memory samples once per second, so this only trips on
	// a stalled connection.
	ReadTimeout = 30 * time.Second
)

// Stream is an open websocket connection to the controller.
type Stream struct {
	conn *websocket.Conn
}

// Close terminates the stream.
func (s *Stream) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

// ReadJSON reads one message and decodes it into out.
//
// A malformed frame is skipped instead of failing the whole stream so a
// single bad line cannot take the dashboard down.
func (s *Stream) ReadJSON(ctx context.Context, out any) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(ReadTimeout))
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		if err := json.Unmarshal(data, out); err != nil {
			continue
		}
		return nil
	}
}

// DialStream opens a websocket stream on the given controller path.
//
// The controller pushes newline delimited JSON frames over the socket.
func (c *Client) DialStream(ctx context.Context, path string, query url.Values) (*Stream, error) {
	endpoint, err := c.websocketURL(path, query)
	if err != nil {
		return nil, err
	}
	dialer := &websocket.Dialer{
		Proxy:            nil,
		HandshakeTimeout: HandshakeTimeout,
	}
	header := http.Header{}
	if c.secret != "" {
		header.Set("Authorization", "Bearer "+c.secret)
	}
	conn, response, err := dialer.DialContext(ctx, endpoint, header)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("websocket dial %s failed: %w (HTTP %s)", path, err, response.Status)
		}
		return nil, fmt.Errorf("websocket dial %s failed: %w", path, err)
	}
	// The controller pushes on its own schedule; a long deadline only
	// protects against a silently dead socket.
	conn.SetReadDeadline(time.Now().Add(ReadTimeout))
	return &Stream{conn: conn}, nil
}

func (c *Client) websocketURL(path string, query url.Values) (string, error) {
	parsed, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "https", "wss":
		parsed.Scheme = "wss"
	default:
		parsed.Scheme = "ws"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	if len(query) > 0 {
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}
