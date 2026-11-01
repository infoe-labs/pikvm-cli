package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/cli"
)

func TestNamedWebSocketCommandsAndAllEventParameters(t *testing.T) {
	for _, tc := range []struct {
		args        []string
		path, event string
		fields      map[string]any
	}{
		{[]string{"events", "ping"}, "/api/ws", "ping", map[string]any{}},
		{[]string{"events", "key", "--key", "KeyA", "--state=false", "--finish"}, "/api/ws", "key", map[string]any{"key": "KeyA", "state": false, "finish": true}},
		{[]string{"events", "button", "--button", "left", "--state=true"}, "/api/ws", "mouse_button", map[string]any{"button": "left", "state": true}},
		{[]string{"events", "move", "--to-x", "-10", "--to-y", "20"}, "/api/ws", "mouse_move", map[string]any{"to": map[string]int{"x": -10, "y": 20}}},
		{[]string{"events", "relative", "--delta", `[{"x":1,"y":2},{"x":-3,"y":4}]`, "--squash"}, "/api/ws", "mouse_relative", map[string]any{"delta": []map[string]int{{"x": 1, "y": 2}, {"x": -3, "y": 4}}, "squash": true}},
		{[]string{"events", "wheel", "--delta", `{"x":0,"y":1}`}, "/api/ws", "mouse_wheel", map[string]any{"delta": map[string]int{"x": 0, "y": 1}, "squash": false}},
		{[]string{"media", "start", "--type", "video", "--format", "h264"}, "/api/media/ws", "start", map[string]any{"type": "video", "format": "h264"}},
	} {
		t.Run(fmt.Sprint(tc.args), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("wrong path %s", r.URL.Path)
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
				var got, want any
				_ = json.Unmarshal(data, &got)
				expected, _ := json.Marshal(map[string]any{"event_type": tc.event, "event": tc.fields})
				_ = json.Unmarshal(expected, &want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("event fields changed %s", data)
				}
				_ = socket.Write(r.Context(), typ, data)
			}))
			defer s.Close()
			root := cli.NewRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			args := append([]string{"--url", s.URL}, tc.args...)
			args = append(args, "--count", "1")
			root.SetArgs(args)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := root.ExecuteContext(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestBinaryWebSocketCommands(t *testing.T) {
	for opcode := byte(0); opcode <= 5; opcode++ {
		t.Run(fmt.Sprint(opcode), func(t *testing.T) {
			frame := []byte{opcode, 0xff, 0, 0x7f}
			file := filepath.Join(t.TempDir(), "frame.bin")
			if e := os.WriteFile(file, frame, 0600); e != nil {
				t.Fatal(e)
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				if typ != websocket.MessageBinary || !bytes.Equal(data, frame) {
					t.Error("binary frame changed")
				}
				_ = socket.Write(r.Context(), typ, data)
			}))
			defer s.Close()
			root := cli.NewRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"--url", s.URL, "events", "binary", "--binary-input", file, "--count", "1"})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := root.ExecuteContext(ctx); e != nil {
				t.Fatal(e)
			}
			var result map[string]string
			if e := json.Unmarshal(out.Bytes(), &result); e != nil {
				t.Fatal(e)
			}
			if result["data_base64"] != base64.StdEncoding.EncodeToString(frame) {
				t.Fatal("binary output changed")
			}
		})
	}
}
