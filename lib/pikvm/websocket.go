package pikvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/coder/websocket"
)

// Message retains a WebSocket message and its text/binary type.
// Binary media frames include PiKVM's protocol header; Data is not decoded.
type Message struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}

// Session is a context-aware WebSocket connection. Close it after use.
// It supports concurrent reads and writes. The initial read limit is 16 MiB.
type Session struct{ conn *websocket.Conn }

// Dial opens an authenticated WebSocket API path, such as /api/ws or /api/media/ws.
// options holds all handshake query parameters; HTTP timeouts apply to the handshake.
func (c *Client) Dial(ctx context.Context, path string, options url.Values) (*Session, error) {
	u, err := c.resolveURL(path, options)
	if err != nil {
		return nil, err
	}
	req := &http.Request{URL: u, Header: make(http.Header)}
	c.authenticate(req)
	conn, resp, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: req.Header})
	if err != nil {
		if resp != nil {
			if resp.Body != nil {
				defer resp.Body.Close()
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
			}
			return nil, &APIError{resp.StatusCode, "GET", path, http.StatusText(resp.StatusCode)}
		}
		return nil, fmt.Errorf("PiKVM WebSocket %s: %w", path, safeTransportError(err))
	}
	conn.SetReadLimit(16 << 20)
	return &Session{conn: conn}, nil
}

func (s *Session) Read(ctx context.Context) (Message, error) {
	typ, data, err := s.conn.Read(ctx)
	if err != nil {
		return Message{}, safeTransportError(err)
	}
	name := "text"
	if typ == websocket.MessageBinary {
		name = "binary"
	}
	return Message{Type: name, Data: data}, nil
}

// SetReadLimit changes the maximum received message size. A negative limit disables it.
func (s *Session) SetReadLimit(limit int64) { s.conn.SetReadLimit(limit) }

// Close closes immediately, including when the remote peer is unresponsive.
func (s *Session) Close() error { return s.conn.CloseNow() }

// SendEvent sends PiKVM's JSON event envelope, preserving all event fields.
func (s *Session) SendEvent(ctx context.Context, eventType string, event any) error {
	if eventType == "" {
		return fmt.Errorf("event type is required")
	}
	if event == nil {
		event = struct{}{}
	}
	data, err := json.Marshal(struct {
		Type  string `json:"event_type"`
		Event any    `json:"event"`
	}{eventType, event})
	if err != nil {
		return err
	}
	return socketWrite(s, ctx, websocket.MessageText, data)
}

// SendJSON sends an already encoded JSON event envelope.
func (s *Session) SendJSON(ctx context.Context, data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON event")
	}
	return socketWrite(s, ctx, websocket.MessageText, data)
}

// SendBinary sends a complete binary frame, including its first opcode byte.
// KVMD opcodes are 0 (ping), 1 (key), 2 (button), 3 (move), 4 (relative),
// and 5 (wheel). Media uses 0 (ping) and 1 (request keyframe).
func (s *Session) SendBinary(ctx context.Context, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("binary frame requires an opcode")
	}
	return socketWrite(s, ctx, websocket.MessageBinary, data)
}

func socketWrite(s *Session, ctx context.Context, typ websocket.MessageType, data []byte) error {
	if err := s.conn.Write(ctx, typ, data); err != nil {
		return safeTransportError(err)
	}
	return nil
}
