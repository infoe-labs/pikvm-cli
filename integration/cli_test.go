package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

var binary string

func TestMain(m *testing.M) {
	dir, e := os.MkdirTemp("", "pikvm-integration-*")
	if e != nil {
		fmt.Fprintln(os.Stderr, "cannot create integration directory")
		os.Exit(1)
	}
	binary = filepath.Join(dir, "pikvm-cli")
	cmd := exec.Command("go", "build", "-race", "-ldflags", "-X main.version=integration-test", "-o", binary, "../cmd/pikvm-cli")
	if e = cmd.Run(); e != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "cannot build integration executable")
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
func execute(t *testing.T, input string, env map[string]string, args ...string) ([]byte, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	// Tests never inherit a developer's real PiKVM connection or credentials.
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PIKVM_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(input)
	e := cmd.Run()
	return out.Bytes(), stderr.Bytes(), e
}
func TestExecutableEnvironmentAuthenticationAndOutput(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/info" || r.URL.Query().Get("fields") != "hw" || r.Header.Get("X-KVMD-User") != "operator" || r.Header.Get("X-KVMD-Passwd") != "synthetic-password" {
			t.Error("executable request changed")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{"model":"synthetic"}}`)
	}))
	defer server.Close()
	env := map[string]string{"PIKVM_URL": server.URL, "PIKVM_USER": "operator", "PIKVM_PASSWORD": "synthetic-password"}
	out, stderr, e := execute(t, "", env, "--insecure", "system", "info", "--fields", "hw")
	if e != nil || len(stderr) != 0 {
		t.Fatal("executable failed")
	}
	var result map[string]any
	if json.Unmarshal(out, &result) != nil || result["ok"] != true {
		t.Error("invalid JSON output")
	}
	_, stderr, e = execute(t, "", env, "system", "info", "--fields", "hw")
	if e == nil || len(stderr) == 0 {
		t.Error("self-signed TLS unexpectedly trusted")
	}
	if bytes.Contains(stderr, []byte(server.URL)) || bytes.Contains(stderr, []byte("synthetic-password")) {
		t.Error("stderr exposed connection details")
	}
}
func TestExecutableBinaryUploadSnapshotAndFailure(t *testing.T) {
	image := []byte{0xff, 0xd8, 0, 1, 0xff, 0xd9}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/msd/write":
			data, _ := io.ReadAll(r.Body)
			if !bytes.Equal(data, image) || r.ContentLength != int64(len(image)) {
				t.Error("stdin upload corrupted")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/api/streamer/snapshot":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(image)
		default:
			http.Error(w, "synthetic-private-response", 403)
		}
	}))
	defer server.Close()
	env := map[string]string{"PIKVM_URL": server.URL}
	if _, _, e := execute(t, string(image), env, "msd", "upload", "--image", "image.iso", "--input", "-"); e != nil {
		t.Fatal("upload failed")
	}
	output := filepath.Join(t.TempDir(), "snapshot.jpg")
	if out, _, e := execute(t, "", env, "streamer", "snapshot", "-o", output); e != nil || len(out) != 0 {
		t.Fatal("snapshot failed")
	}
	data, _ := os.ReadFile(output)
	if !bytes.Equal(data, image) {
		t.Error("snapshot corrupted")
	}
	out, stderr, e := execute(t, "", env, "atx", "state")
	if e == nil || len(out) != 0 || !bytes.Contains(stderr, []byte("403")) || bytes.Contains(stderr, []byte("synthetic-private-response")) {
		t.Error("incorrect failure exit/output")
	}
}
func TestExecutableWebSocketPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws" || r.URL.Query().Get("stream") != "false" {
			t.Error("incorrect WebSocket request")
		}
		socket, e := websocket.Accept(w, r, nil)
		if e != nil {
			t.Error(e)
			return
		}
		defer socket.CloseNow()
		_, data, e := socket.Read(r.Context())
		if e != nil {
			t.Error(e)
			return
		}
		var event map[string]any
		_ = json.Unmarshal(data, &event)
		if event["event_type"] != "ping" {
			t.Error("ping missing")
		}
		_ = socket.Write(r.Context(), websocket.MessageText, []byte(`{"event_type":"pong","event":{}}`))
	}))
	defer server.Close()
	out, _, e := execute(t, "", map[string]string{"PIKVM_URL": server.URL}, "events", "connect", "--stream", "false", "--event-type", "ping", "--count", "1")
	if e != nil || !bytes.Contains(out, []byte("pong")) {
		t.Error("WebSocket process integration failed")
	}
}
func TestExecutableVersionAndValidation(t *testing.T) {
	out, _, e := execute(t, "", nil, "--version")
	if e != nil || !bytes.Contains(out, []byte("integration-test")) {
		t.Error("version missing")
	}
	for _, args := range [][]string{{"system", "info"}, {"unknown-command"}, {"hid", "move", "--to-x", "1"}} {
		out, stderr, e := execute(t, "", nil, args...)
		if e == nil || len(out) != 0 || len(stderr) == 0 {
			t.Error("validation did not set failure status")
		}
	}
}
