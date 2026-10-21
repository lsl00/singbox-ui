package singbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrTimeout reports that a stream had nothing to deliver before the
// deadline.
//
// The connection and the log stream are event driven, so an idle instance
// stays silent for a long time. A timeout is therefore not an error of
// the instance, but the stream has to be opened again afterwards.
var ErrTimeout = errors.New("stream timed out")

// frameStream is an open server side stream of the gRPC-Web API.
type frameStream struct {
	body   io.ReadCloser
	parser frameParser
	cancel context.CancelFunc
}

// Close terminates the stream.
func (s *frameStream) Close() error {
	s.cancel()
	return s.body.Close()
}

// next waits for the next frame of the stream.
//
// The timeout is a liveness check: the instance pushes a message on its own
// schedule, so a missing message means the connection died. The stream is
// closed in that case and the caller has to reconnect.
func (s *frameStream) next(timeout time.Duration) ([]byte, error) {
	timer := time.AfterFunc(timeout, s.cancel)
	defer timer.Stop()

	buffer := make([]byte, 32*1024)
	for {
		if frame, ok := s.parser.pop(); ok {
			return frame, nil
		}
		count, err := s.body.Read(buffer)
		if count > 0 {
			if parseErr := s.parser.feed(buffer[:count]); parseErr != nil {
				return nil, parseErr
			}
			continue
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, ErrTimeout
			}
			return nil, err
		}
	}
}

// nextLatest waits for the next frame and discards the older ones.
//
// The newest frame wins: the instance pushes a message every interval and a
// slower reader would otherwise show an ever growing backlog.
func (s *frameStream) nextLatest(timeout time.Duration) ([]byte, error) {
	timer := time.AfterFunc(timeout, s.cancel)
	defer timer.Stop()

	buffer := make([]byte, 32*1024)
	for {
		if frame, ok := s.parser.latestFrame(); ok {
			return frame, nil
		}
		count, err := s.body.Read(buffer)
		if count > 0 {
			if parseErr := s.parser.feed(buffer[:count]); parseErr != nil {
				return nil, parseErr
			}
			continue
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, ErrTimeout
			}
			return nil, err
		}
	}
}

// openStream subscribes to a server side stream of the instance.
func (c *Client) openStream(ctx context.Context, method string, body []byte) (*frameStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	request, err := c.request(streamCtx, method, body)
	if err != nil {
		cancel()
		return nil, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("gRPC request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		cancel()
		response.Body.Close()
		return nil, fmt.Errorf("HTTP error: %s", response.Status)
	}
	return &frameStream{body: response.Body, cancel: cancel}, nil
}

// StatusStream is an open subscription of the status stream.
type StatusStream struct {
	*frameStream
	interval time.Duration
}

// OpenStatusStream subscribes to the status stream of the instance.
//
// The stream stays open until the context is cancelled or the instance
// closes the connection.
func (c *Client) OpenStatusStream(ctx context.Context, interval time.Duration) (*StatusStream, error) {
	if interval <= 0 {
		interval = DefaultStatusInterval
	}
	body := appendInt64(nil, 1, int64(interval))
	stream, err := c.openStream(ctx, "SubscribeStatus", body)
	if err != nil {
		return nil, err
	}
	return &StatusStream{frameStream: stream, interval: interval}, nil
}

// Next blocks until a status sample is available.
//
// The newest sample wins, because the instance pushes a sample every
// interval and a slower reader would otherwise show a growing backlog.
func (s *StatusStream) Next(timeout time.Duration) (Status, error) {
	frame, err := s.nextLatest(timeout)
	if err != nil {
		return Status{}, err
	}
	status, err := decodeStatus(frame)
	if err != nil {
		return Status{}, err
	}
	status.Interval = s.interval
	return status, nil
}

// ConnectionStream is an open subscription of the connection stream.
type ConnectionStream struct {
	*frameStream
}

// OpenConnectionStream subscribes to the connection stream of the instance.
//
// The instance pushes a batch of connection events per interval.
func (c *Client) OpenConnectionStream(ctx context.Context, interval time.Duration) (*ConnectionStream, error) {
	if interval <= 0 {
		interval = DefaultStatusInterval
	}
	body := encodeSubscribeConnectionsRequest(interval)
	stream, err := c.openStream(ctx, "SubscribeConnections", body)
	if err != nil {
		return nil, err
	}
	return &ConnectionStream{frameStream: stream}, nil
}

// Next blocks until the next batch of connection events is available.
func (s *ConnectionStream) Next(timeout time.Duration) (ConnectionEvents, error) {
	frame, err := s.next(timeout)
	if err != nil {
		return ConnectionEvents{}, err
	}
	return decodeConnectionEvents(frame)
}

// LogStream is an open subscription of the log stream.
type LogStream struct {
	*frameStream
}

// OpenLogStream subscribes to the log stream of the instance.
func (c *Client) OpenLogStream(ctx context.Context) (*LogStream, error) {
	stream, err := c.openStream(ctx, "SubscribeLog", nil)
	if err != nil {
		return nil, err
	}
	return &LogStream{frameStream: stream}, nil
}

// Next blocks until the next batch of log records is available.
func (s *LogStream) Next(timeout time.Duration) (Log, error) {
	frame, err := s.next(timeout)
	if err != nil {
		return Log{}, err
	}
	return decodeLog(frame)
}

// CloseConnection closes a single connection of the instance.
func (c *Client) CloseConnection(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body := encodeCloseConnectionRequest(id)
	request, err := c.request(ctx, "CloseConnection", body)
	if err != nil {
		return err
	}
	return c.finish(request)
}

// CloseAllConnections closes every tracked connection of the instance.
func (c *Client) CloseAllConnections(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := c.request(ctx, "CloseAllConnections", nil)
	if err != nil {
		return err
	}
	return c.finish(request)
}

// finish reads the response of a unary call that has no payload.
func (c *Client) finish(request *http.Request) error {
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("gRPC request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP error: %s", response.Status)
	}
	_, err = io.Copy(io.Discard, io.LimitReader(response.Body, maxFrameSize))
	return err
}
