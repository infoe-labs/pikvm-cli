package pikvm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

func TestEveryWebSocketEventAndBinaryOpcode(t *testing.T) {
	cases := []struct {
		name, path, event string
		payload           any
		send              func(context.Context, *pikvm.Session) error
	}{
		{"ping", "/api/ws", "ping", map[string]any{}, func(ctx context.Context, s *pikvm.Session) error { return s.Ping(ctx) }},
		{"key", "/api/ws", "key", map[string]any{"key": "KeyA", "state": false, "finish": true}, func(ctx context.Context, s *pikvm.Session) error { return s.Key(ctx, "KeyA", false, true) }},
		{"button", "/api/ws", "mouse_button", map[string]any{"button": "right", "state": true}, func(ctx context.Context, s *pikvm.Session) error { return s.MouseButton(ctx, "right", true) }},
		{"move", "/api/ws", "mouse_move", map[string]any{"to": map[string]int{"x": -100, "y": 200}}, func(ctx context.Context, s *pikvm.Session) error { return s.MouseMove(ctx, -100, 200) }},
		{"relative", "/api/ws", "mouse_relative", map[string]any{"delta": []pikvm.Delta{{X: -2, Y: 3}, {X: 4, Y: -5}}, "squash": true}, func(ctx context.Context, s *pikvm.Session) error {
			return s.MouseRelative(ctx, []pikvm.Delta{{X: -2, Y: 3}, {X: 4, Y: -5}}, true)
		}},
		{"wheel", "/api/ws", "mouse_wheel", map[string]any{"delta": []pikvm.Delta{{X: 0, Y: 2}}, "squash": false}, func(ctx context.Context, s *pikvm.Session) error {
			return s.MouseWheel(ctx, []pikvm.Delta{{X: 0, Y: 2}}, false)
		}},
		{"start", "/api/media/ws", "start", map[string]any{"type": "video", "format": "h264"}, func(ctx context.Context, s *pikvm.Session) error { return s.StartMedia(ctx, "video", "h264") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := echoSocketServer(t, tc.path)
			defer server.Close()
			client, _ := pikvm.New(pikvm.Config{URL: server.URL, Username: "user", Password: "password"})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			session, e := client.Dial(ctx, tc.path, url.Values{"stream": {"0"}})
			if e != nil {
				t.Fatal(e)
			}
			defer session.Close()
			if e = tc.send(ctx, session); e != nil {
				t.Fatal(e)
			}
			message, e := session.Read(ctx)
			if e != nil {
				t.Fatal(e)
			}
			var got, want any
			_ = json.Unmarshal(message.Data, &got)
			data, _ := json.Marshal(map[string]any{"event_type": tc.event, "event": tc.payload})
			_ = json.Unmarshal(data, &want)
			if message.Type != "text" || !reflect.DeepEqual(got, want) {
				t.Fatalf("wrong event %s", message.Data)
			}
		})
	}
	for _, path := range []string{"/api/ws", "/api/media/ws"} {
		maxOpcode := byte(5)
		if path == "/api/media/ws" {
			maxOpcode = 1
		}
		for opcode := byte(0); opcode <= maxOpcode; opcode++ {
			t.Run(path+" binary "+string(rune('0'+opcode)), func(t *testing.T) {
				server := echoSocketServer(t, path)
				defer server.Close()
				client, _ := pikvm.New(pikvm.Config{URL: server.URL, Username: "user", Password: "password"})
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				session, e := client.Dial(ctx, path, nil)
				if e != nil {
					t.Fatal(e)
				}
				defer session.Close()
				frame := []byte{opcode, 0xff, 0, 0x7f}
				if e = session.SendBinary(ctx, frame); e != nil {
					t.Fatal(e)
				}
				message, e := session.Read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if message.Type != "binary" || !bytes.Equal(message.Data, frame) {
					t.Fatal("binary frame changed")
				}
			})
		}
	}
}
func echoSocketServer(t *testing.T, path string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.Header.Get("X-KVMD-User") != "user" || r.Header.Get("X-KVMD-Passwd") != "password" {
			t.Error("incorrect websocket path or authentication")
		}
		socket, e := websocket.Accept(w, r, nil)
		if e != nil {
			t.Error(e)
			return
		}
		defer socket.CloseNow()
		typ, data, e := socket.Read(r.Context())
		if e != nil {
			t.Error(e)
			return
		}
		if e = socket.Write(r.Context(), typ, data); e != nil {
			t.Error(e)
		}
	}))
}
