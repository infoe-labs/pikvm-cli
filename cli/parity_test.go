package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/cli"
	"github.com/infoe-labs/pikvm-cli/internal/apitest"
)

func flattenJSON(fields map[string]any, prefix string, out map[string]any) {
	for k, v := range fields {
		if prefix != "" {
			k = prefix + "." + k
		}
		if child, ok := v.(map[string]any); ok {
			flattenJSON(child, k, out)
		} else {
			out[k] = v
		}
	}
}
func TestEveryCLIEndpointAndParameterOnTheWire(t *testing.T) {
	catalog := apitest.Catalog(t)
	for _, op := range apitest.Inventory(t) {
		t.Run(apitest.Key(op.Method, op.Path), func(t *testing.T) {
			ep := catalog[apitest.Key(op.Method, op.Path)]
			args := []string{ep.Group, ep.Name}
			wantQuery := url.Values{}
			wantFields := map[string]any{}
			wantRaw := ""
			if strings.Contains(op.Path, "{id}") {
				args = append(args, "--id", "2")
			}
			if op.Location == "query" {
				for _, p := range op.Parameters {
					v := apitest.Sample(p)
					wantQuery.Set(p, v)
					args = append(args, "--"+ep.QueryFlag(p), v)
				}
			} else {
				flags := map[string]struct{ flag, typ string }{}
				for _, p := range ep.BodyParameters {
					flags[p.Name] = struct{ flag, typ string }{p.Flag, p.Type}
				}
				for _, p := range op.Parameters {
					param, ok := flags[p]
					if !ok {
						t.Fatalf("missing body flag for %s", p)
					}
					v := apitest.Sample(p)
					var expected any = v
					if param.typ == "bool" {
						v = "false"
						expected = false
					}
					wantFields[p] = expected
					args = append(args, "--"+param.flag, v)
				}
			}
			if ep.BodyRequired && len(ep.BodyParameters) == 0 {
				wantRaw = "\x00payload\n"
				args = append(args, "--body", wantRaw)
			}
			if ep.WebSocket {
				args = append(args, "--count", "1")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != op.Method || r.URL.Path != strings.ReplaceAll(op.Path, "{id}", "2") {
					t.Errorf("wrong route %s %s", r.Method, r.URL.Path)
				}
				if !reflect.DeepEqual(r.URL.Query(), wantQuery) {
					t.Errorf("wrong query %v want %v", r.URL.Query(), wantQuery)
				}
				if r.Header.Get("X-KVMD-User") != "device-user" || r.Header.Get("X-KVMD-Passwd") != "device-secret" {
					t.Error("API parameter flags overwrote device credentials")
				}
				if ep.WebSocket {
					socket, e := websocket.Accept(w, r, nil)
					if e != nil {
						t.Error(e)
						return
					}
					defer socket.CloseNow()
					_ = socket.Write(r.Context(), websocket.MessageText, []byte(`{"event_type":"loop","event":{}}`))
					_, _, _ = socket.Read(r.Context())
					return
				}
				data, _ := io.ReadAll(r.Body)
				switch op.Location {
				case "form":
					values, e := url.ParseQuery(string(data))
					if e != nil {
						t.Error(e)
					}
					fields := map[string]any{}
					for k, v := range values {
						fields[k] = v[0]
					}
					if !reflect.DeepEqual(fields, wantFields) {
						t.Errorf("wrong form %v want %v", fields, wantFields)
					}
					http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session", Path: "/"})
				case "json":
					var fields map[string]any
					if e := json.Unmarshal(data, &fields); e != nil {
						t.Error(e)
					}
					flat := map[string]any{}
					flattenJSON(fields, "", flat)
					if !reflect.DeepEqual(flat, wantFields) {
						t.Errorf("wrong JSON fields %v want %v", flat, wantFields)
					}
				default:
					if string(data) != wantRaw {
						t.Errorf("unexpected body %q", data)
					}
				}
				if ep.ContentType != "" && r.Header.Get("Content-Type") != ep.ContentType {
					t.Errorf("wrong content type %s", r.Header.Get("Content-Type"))
				}
				if ep.Path == "/api/msd/write" && r.ContentLength != int64(len(wantRaw)) {
					t.Errorf("missing upload Content-Length: %d", r.ContentLength)
				}
				if ep.StreamResponse {
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = io.WriteString(w, "\x00\xffstream")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
				}
			}))
			defer server.Close()
			root := cli.NewRootCommand()
			var output bytes.Buffer
			root.SetOut(&output)
			root.SetArgs(append([]string{"--url", server.URL, "--user", "device-user", "--password", "device-secret"}, args...))
			command, _, e := root.Find([]string{ep.Group, ep.Name})
			if e != nil || command.Name() != ep.Name {
				t.Fatalf("missing CLI command for %s", op.Path)
			}
			if e = root.Execute(); e != nil {
				t.Fatal(e)
			}
			if ep.StreamResponse && output.String() != "\x00\xffstream" {
				t.Fatalf("stream changed: %q", output.String())
			}
		})
	}
}
func TestFetchRelaysEveryNDJSONRecordAndReportsLateFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			records := "{\"ok\":true,\"result\":{\"written\":0}}\n{\"ok\":true,\"result\":{\"written\":42}}\n"
			if failure {
				records += "{\"ok\":false,\"result\":{\"error\":\"download failed\"}}\n"
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = io.WriteString(w, records)
			}))
			defer s.Close()
			root := cli.NewRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"--url", s.URL, "msd", "fetch", "--source-url", "https://example.com/image.iso"})
			e := root.Execute()
			if (e != nil) != failure {
				t.Fatalf("unexpected error %v", e)
			}
			if out.String() != records {
				t.Errorf("lost or changed progress records: %q", out.String())
			}
		})
	}
}
