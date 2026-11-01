package pikvm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/internal/apitest"
	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

func TestCompleteUpstreamEndpointAndParameterCoverage(t *testing.T) {
	catalog := apitest.Catalog(t)
	inventory := apitest.Inventory(t)
	if len(catalog) != len(inventory) {
		t.Fatalf("catalog has %d routes, upstream has %d", len(catalog), len(inventory))
	}
	for _, op := range inventory {
		t.Run(apitest.Key(op.Method, op.Path), func(t *testing.T) {
			ep, ok := catalog[apitest.Key(op.Method, op.Path)]
			if !ok {
				t.Fatal("upstream operation missing")
			}
			var query, fields []string
			for _, p := range ep.Parameters {
				p = strings.TrimSuffix(p, "*")
				if p == "id" && strings.Contains(op.Path, "{id}") {
					continue
				}
				query = append(query, p)
			}
			for _, p := range ep.BodyParameters {
				fields = append(fields, p.Name)
			}
			if op.Location == "query" {
				apitest.AssertSame(t, query, op.Parameters)
				apitest.AssertSame(t, fields, nil)
			} else {
				apitest.AssertSame(t, query, nil)
				apitest.AssertSame(t, fields, op.Parameters)
			}
		})
	}
}

// Every named service method is called with every independently inventoried
// parameter. The server checks the resulting HTTP request, including uploads.
func TestEveryServiceMethodOnTheWire(t *testing.T) {
	catalog := apitest.Catalog(t)
	serviceNames := map[string]string{"hid": "HID", "atx": "ATX", "msd": "MSD", "gpio": "GPIO"}
	for _, op := range apitest.Inventory(t) {
		t.Run(apitest.Key(op.Method, op.Path), func(t *testing.T) {
			ep := catalog[apitest.Key(op.Method, op.Path)]
			query := url.Values{}
			var body string
			if op.Location == "query" {
				for _, p := range op.Parameters {
					query.Set(p, apitest.Sample(p))
				}
			} else if op.Location == "form" {
				f := url.Values{}
				for _, p := range op.Parameters {
					f.Set(p, apitest.Sample(p))
				}
				body = f.Encode()
			} else {
				fields := map[string]any{}
				for _, p := range op.Parameters {
					fields[p] = apitest.Sample(p)
				}
				b, _ := json.Marshal(fields)
				body = string(b)
			}
			if ep.BodyRequired && len(ep.BodyParameters) == 0 {
				body = "\x00\xffupload or text\n"
			}
			var called atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called.Store(true)
				if r.Method != op.Method || r.URL.Path != strings.ReplaceAll(op.Path, "{id}", "2") {
					t.Errorf("wrong route %s %s", r.Method, r.URL.Path)
				}
				if !reflect.DeepEqual(r.URL.Query(), query) {
					t.Errorf("wrong query %v want %v", r.URL.Query(), query)
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
				if string(data) != body {
					t.Errorf("body changed: %q want %q", data, body)
				}
				if ep.ContentType != "" && r.Header.Get("Content-Type") != ep.ContentType {
					t.Errorf("wrong content type %s", r.Header.Get("Content-Type"))
				}
				if ep.Path == "/api/msd/write" && r.ContentLength != int64(len(body)) {
					t.Errorf("upload has wrong Content-Length %d", r.ContentLength)
				}
				if ep.StreamResponse {
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = io.WriteString(w, "\x00\xffresponse")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
				}
			}))
			defer server.Close()
			client, e := pikvm.New(pikvm.Config{URL: server.URL})
			if e != nil {
				t.Fatal(e)
			}
			serviceName := serviceNames[ep.Group]
			if serviceName == "" {
				serviceName = strings.ToUpper(ep.Group[:1]) + ep.Group[1:]
			}
			serviceFactory := reflect.ValueOf(client).MethodByName(serviceName)
			if !serviceFactory.IsValid() {
				t.Fatalf("missing service %s", serviceName)
			}
			service := serviceFactory.Call(nil)[0]
			methodName := ""
			for _, word := range strings.Split(ep.Name, "-") {
				methodName += strings.ToUpper(word[:1]) + word[1:]
			}
			method := service.MethodByName(methodName)
			if !method.IsValid() {
				t.Fatalf("missing %s.%s", serviceName, methodName)
			}
			args := []reflect.Value{reflect.ValueOf(context.Background())}
			if strings.Contains(op.Path, "{id}") {
				args = append(args, reflect.ValueOf("2"))
			}
			args = append(args, reflect.ValueOf(query))
			if ep.ContentType != "" {
				args = append(args, reflect.ValueOf(strings.NewReader(body)))
			}
			results := method.Call(args)
			if !results[1].IsNil() {
				t.Fatal(results[1].Interface())
			}
			switch result := results[0].Interface().(type) {
			case *http.Response:
				data, e := io.ReadAll(result.Body)
				_ = result.Body.Close()
				if e != nil {
					t.Fatal(e)
				}
				if ep.StreamResponse && string(data) != "\x00\xffresponse" {
					t.Error("stream response corrupted")
				}
			case *pikvm.Session:
				if _, e := result.Read(context.Background()); e != nil {
					t.Error(e)
				}
				_ = result.Close()
			}
			if !called.Load() {
				t.Fatal("service did not make a request")
			}
		})
	}
}
