package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

var errSynthetic = errors.New("synthetic failure")

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errSynthetic }

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errSynthetic }
func TestPublicErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{context.Canceled, "operation canceled"}, {context.DeadlineExceeded, "operation timed out"}, {&fs.PathError{Op: "open", Path: "synthetic-private-name", Err: fs.ErrPermission}, "file operation failed; check file access and available storage"}, {errSynthetic, "synthetic failure"}} {
		if got := ErrorMessage(tc.err); got != tc.want {
			t.Fatalf("unexpected error message %q", got)
		}
	}
}
func TestResponseFormatsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, content, body string
		status              int
		invalid             bool
	}{{"json", "application/json", `{"n":9007199254740993}`, 200, false}, {"text", "text/plain", "hello", 200, false}, {"empty", "application/json", "", 204, false}, {"broken json", "application/json", "{", 200, true}, {"ndjson", "application/x-ndjson", "{\"ok\":true}\n{\"ok\":false}", 200, true}} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			r := &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.content}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			e := writeResponse(cmd, r, "")
			if (e != nil) != tc.invalid {
				t.Fatalf("unexpected response error %v", e)
			}
			if tc.name == "json" && !strings.Contains(out.String(), "9007199254740993") {
				t.Error("lost integer precision")
			}
		})
	}
	for _, ct := range []string{"text/plain", "application/json", "application/x-ndjson"} {
		cmd := &cobra.Command{}
		cmd.SetOut(failedWriter{})
		r := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}
		if !errors.Is(writeResponse(cmd, r, ""), errSynthetic) {
			t.Error("writer error was lost")
		}
	}
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	for _, ct := range []string{"text/plain", "application/x-ndjson"} {
		r := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(failedReader{})}
		if !errors.Is(writeResponse(cmd, r, ""), errSynthetic) {
			t.Error("reader error was lost")
		}
	}
	file := filepath.Join(t.TempDir(), "response.json")
	r := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("raw response"))}
	if e := writeResponse(cmd, r, file); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(file)
	if string(data) != "raw response" {
		t.Error("file output changed")
	}
	r.Body = io.NopCloser(failedReader{})
	if !errors.Is(writeResponse(cmd, r, file), errSynthetic) {
		t.Error("file copy failure was lost")
	}
	r.Body = io.NopCloser(strings.NewReader(""))
	if writeResponse(cmd, r, t.TempDir()) == nil {
		t.Error("accepted directory output")
	}
}
func TestClientConfigurationAndInputFailures(t *testing.T) {
	for _, cfg := range []pikvm.Config{{}, {URL: "https://pikvm.example.com", Timeout: -1}, {URL: "ftp://pikvm.example.com"}} {
		if _, e := newClient(cfg); e == nil {
			t.Fatal("accepted invalid config")
		}
	}
	if _, e := newClient(pikvm.Config{URL: "https://pikvm.example.com"}); e != nil {
		t.Fatal(e)
	}
	cmd := &cobra.Command{}
	if _, done, e := openInput(cmd, filepath.Join(t.TempDir(), "missing"), ""); e == nil {
		done()
		t.Error("opened missing input")
	}
	for _, tc := range []struct {
		in   string
		want bool
	}{{"yes", true}, {"NO", false}, {"true", true}, {"0", false}} {
		v, e := parseAPIBool(tc.in)
		if e != nil || v != tc.want {
			t.Error("incorrect boolean parsing")
		}
	}
	if _, e := parseAPIBool("invalid"); e == nil {
		t.Error("accepted malformed boolean")
	}
}
func TestCLIValidationAndHTTPFailures(t *testing.T) {
	for _, args := range [][]string{{"system", "info"}, {"--url", "https://pikvm.example.com", "--request-timeout", "-1s", "system", "info"}, {"--url", "https://pikvm.example.com", "request", "GET", "/api/info", "-q", "missing-equals"}, {"--url", "https://pikvm.example.com", "request", "GET", "/api/info", "-q", "=value"}, {"--url", "https://pikvm.example.com", "redfish", "power", "--id", "/", "--reset-type", "On"}, {"--url", "https://pikvm.example.com", "redfish", "insert", "--image", "image.iso", "--inserted", "invalid"}, {"--url", "https://pikvm.example.com", "redfish", "insert", "--image", "image.iso", "--body", "{}"}, {"--url", "https://pikvm.example.com", "events", "connect", "--count", "-1"}, {"--url", "https://pikvm.example.com", "events", "connect", "--video", "jpeg"}, {"--url", "https://pikvm.example.com", "media", "connect", "--stream", "false"}, {"--url", "https://pikvm.example.com", "events", "connect", "--body", "invalid"}, {"--url", "https://pikvm.example.com", "events", "connect", "--event-type", "ping", "--event", "invalid"}, {"--url", "https://pikvm.example.com", "events", "binary"}} {
		root := NewRootCommand()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(args)
		if e := root.Execute(); e == nil {
			t.Errorf("validation accepted %v", args)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "synthetic private response", 403) }))
	defer server.Close()
	for _, args := range [][]string{{"system", "info"}, {"request", "GET", "/api/info"}, {"events", "connect"}} {
		root := NewRootCommand()
		root.SetArgs(append([]string{"--url", server.URL}, args...))
		if e := root.Execute(); e == nil {
			t.Error("HTTP failure was lost")
		}
	}
}

func TestMain(m *testing.M) {
	for _, name := range []string{"PIKVM_URL", "PIKVM_USER", "PIKVM_PASSWORD", "PIKVM_TOKEN", "PIKVM_ENV_FILE", "PIKVM_INSECURE", "PIKVM_AUTH_MODE", "PIKVM_REQUEST_TIMEOUT"} {
		_ = os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

func TestMCPMissingConfigurationAndCleanStdioEOF(t *testing.T) {
	root := NewRootCommand()
	root.SetArgs([]string{"mcp"})
	if root.Execute() == nil {
		t.Error("MCP accepted missing connection configuration")
	}
	root = NewRootCommand()
	root.SetIn(strings.NewReader(""))
	root.SetOut(io.Discard)
	root.SetArgs([]string{"--url", "https://pikvm.example.com", "mcp"})
	if e := root.Execute(); e != nil {
		t.Fatal("MCP did not stop cleanly at stdio EOF")
	}
}
