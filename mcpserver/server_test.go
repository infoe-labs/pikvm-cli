package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T, url string) (context.Context, *sdk.ClientSession) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client, e := pikvm.New(pikvm.Config{URL: url, Username: "operator", Password: "synthetic-password"})
	if e != nil {
		t.Fatal(e)
	}
	server := New(client, "test")
	a, b := sdk.NewInMemoryTransports()
	ss, e := server.Connect(ctx, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, e := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ctx, cs
}
func arguments(ep pikvm.Endpoint) map[string]any {
	args := map[string]any{}
	for _, p := range ep.Parameters {
		p = strings.TrimSuffix(p, "*")
		args[p] = "synthetic"
		if p == "id" {
			args[p] = "2"
		}
	}
	for _, p := range ep.BodyParameters {
		var value any = "synthetic"
		if p.Type == "bool" {
			value = false
		}
		args[p.Name] = value
	}
	if ep.BodyRequired && len(ep.BodyParameters) == 0 {
		args["body"] = "synthetic body"
	}
	return args
}
func TestEveryCatalogToolListsAndCallsThroughSharedAPI(t *testing.T) {
	for _, ep := range pikvm.Endpoints() {
		t.Run(ToolName(ep), func(t *testing.T) {
			var called atomic.Bool
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called.Store(true)
				if r.URL.Path != strings.ReplaceAll(ep.Path, "{id}", "2") || r.Method != ep.Method {
					t.Error("MCP changed API route")
				}
				if r.Header.Get("X-KVMD-User") != "operator" {
					t.Error("configured authentication was not reused")
				}
				if ep.WebSocket {
					s, e := websocket.Accept(w, r, nil)
					if e != nil {
						t.Error(e)
						return
					}
					defer s.CloseNow()
					_ = s.Write(r.Context(), websocket.MessageText, []byte(`{"event_type":"loop","event":{}}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
			}))
			defer backend.Close()
			ctx, cs := connect(t, backend.URL)
			listed, e := cs.ListTools(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			if len(listed.Tools) != len(pikvm.Endpoints()) {
				t.Fatal("MCP tool inventory is incomplete")
			}
			result, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: ToolName(ep), Arguments: arguments(ep)})
			if e != nil || result.IsError {
				t.Fatalf("MCP operation failed: %v", e)
			}
			if !called.Load() {
				t.Fatal("MCP did not use the API")
			}
		})
	}
}
func TestMCPFormatsAndPrivateErrors(t *testing.T) {
	for _, tc := range []struct {
		name, content, body string
		status              int
		isError             bool
	}{
		{"json", "application/json", `{"ok":true}`, 200, false}, {"empty", "application/json", "", 204, false}, {"image", "image/jpeg", "\xff\xd8\xff\xd9", 200, false}, {"text", "text/plain", "synthetic OCR", 200, false}, {"binary", "application/octet-stream", "\x00\xff", 200, false}, {"progress", "application/x-ndjson", "{\"ok\":true}\n{\"ok\":true}\n", 200, false}, {"late failure", "application/x-ndjson", "{\"ok\":false}\n", 200, true}, {"forbidden", "text/plain", "synthetic-private-response", 403, true}, {"oversized", "application/octet-stream", strings.Repeat("x", maxResponseBytes+1), 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.content)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer backend.Close()
			ctx, cs := connect(t, backend.URL)
			result, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "pikvm_system_info", Arguments: map[string]any{}})
			if e != nil {
				t.Fatal(e)
			}
			if result.IsError != tc.isError {
				t.Fatal("MCP failure classification changed")
			}
			if tc.isError {
				encoded, _ := json.Marshal(result)
				if bytes.Contains(encoded, []byte("synthetic-private-response")) || bytes.Contains(encoded, []byte(backend.URL)) {
					t.Fatal("MCP exposed backend details")
				}
			}
			if tc.name == "image" {
				if image, ok := result.Content[0].(*sdk.ImageContent); !ok || !bytes.Equal(image.Data, []byte(tc.body)) {
					t.Error("snapshot was not returned as MCP image")
				}
			}
		})
	}
}
func TestMCPSchemasRejectInvalidArgumentsBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer backend.Close()
	ctx, cs := connect(t, backend.URL)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{{"pikvm_system_info", map[string]any{"unknown": "field"}}, {"pikvm_hid_move", map[string]any{}}, {"pikvm_events_connect", map[string]any{"count": 0}}, {"pikvm_events_connect", map[string]any{"count": 33}}, {"pikvm_redfish_insert", map[string]any{"Image": "image.iso", "Inserted": "invalid"}}} {
		r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
		if e == nil && !r.IsError {
			t.Error("invalid tool input was accepted")
		}
	}
	if calls.Load() != 0 {
		t.Error("invalid input reached device")
	}
}
func TestMCPBodyEncodingAndValidation(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if !bytes.Equal(data, []byte{0, 255, 1}) {
			t.Error("binary upload changed")
		}
		if r.ContentLength != 3 {
			t.Error("shared upload length handling was bypassed")
		}
		w.WriteHeader(204)
	}))
	defer backend.Close()
	ctx, cs := connect(t, backend.URL)
	r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "pikvm_msd_upload", Arguments: map[string]any{"image": "image.iso", "body_base64": base64.StdEncoding.EncodeToString([]byte{0, 255, 1})}})
	if e != nil || r.IsError {
		t.Fatal("MCP upload failed")
	}
	for _, args := range []map[string]any{{"image": "image.iso"}, {"image": "image.iso", "body_base64": "invalid"}, {"image": "image.iso", "body": "text", "body_base64": "AA=="}} {
		r, e = cs.CallTool(ctx, &sdk.CallToolParams{Name: "pikvm_msd_upload", Arguments: args})
		if e != nil || !r.IsError {
			t.Error("invalid body did not return a tool error")
		}
	}
}
func TestMCPWebSocketJSONAndBinaryExchange(t *testing.T) {
	for _, binary := range []bool{false, true} {
		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s, e := websocket.Accept(w, r, nil)
			if e != nil {
				t.Error(e)
				return
			}
			defer s.CloseNow()
			typ, data, e := s.Read(r.Context())
			if e != nil {
				t.Error(e)
				return
			}
			_ = s.Write(r.Context(), typ, data)
		}))
		func() {
			defer backend.Close()
			ctx, cs := connect(t, backend.URL)
			args := map[string]any{"count": 1}
			if binary {
				args["binary_base64"] = "AA=="
			} else {
				args["event_type"] = "ping"
				args["event"] = map[string]any{}
			}
			r, e := cs.CallTool(ctx, &sdk.CallToolParams{Name: "pikvm_events_connect", Arguments: args})
			if e != nil || r.IsError {
				t.Fatal("MCP WebSocket exchange failed")
			}
		}()
	}
}
