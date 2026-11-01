package cli_test

import (
	"bytes"
	"github.com/infoe-labs/pikvm-cli/cli"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommands(t *testing.T) {
	for _, test := range []struct {
		args                                             []string
		path, method, query, body, response, contentType string
	}{
		{[]string{"hid", "type", "--input", "-"}, "/api/hid/print", "POST", "", "hello from stdin", `{"ok":true}`, "application/json"},
		{[]string{"hid", "move", "--to-x", "-32768", "--to-y", "0"}, "/api/hid/events/send_mouse_move", "POST", "to_x=-32768&to_y=0", "", `{"ok":true}`, "application/json"},
		{[]string{"redfish", "power", "--id", "2", "--body", `{"ResetType":"On"}`}, "/redfish/v1/Systems/2/Actions/ComputerSystem.Reset", "POST", "", `{"ResetType":"On"}`, "", ""},
		{[]string{"streamer", "snapshot"}, "/api/streamer/snapshot", "GET", "", "", "\xff\xd8\x00\xff\xd9", "image/jpeg"},
		{[]string{"request", "GET", "/api/info", "-q", "fields=hw"}, "/api/info", "GET", "fields=hw", "", `{"ok":true}`, "application/json"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				if r.URL.Path != test.path || r.Method != test.method || r.URL.RawQuery != test.query || string(b) != test.body {
					t.Errorf("unexpected request %s %s body=%q", r.Method, r.URL, b)
				}
				w.Header().Set("Content-Type", test.contentType)
				if test.response == "" {
					w.WriteHeader(204)
				} else {
					_, _ = io.WriteString(w, test.response)
				}
			}))
			defer server.Close()
			root := cli.NewRootCommand()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetIn(strings.NewReader(test.body))
			root.SetArgs(append([]string{"--url", server.URL}, test.args...))
			if e := root.Execute(); e != nil {
				t.Fatal(e)
			}
			if test.contentType == "image/jpeg" && out.String() != test.response {
				t.Fatal("binary response changed")
			}
		})
	}
}
func TestRequiredFlagsAndBodies(t *testing.T) {
	for _, args := range [][]string{{"atx", "power"}, {"hid", "type"}, {"msd", "upload", "--image", "test.iso"}, {"hid", "move", "--to-x", "0"}} {
		root := cli.NewRootCommand()
		root.SetArgs(append([]string{"--url", "https://pikvm.invalid"}, args...))
		if e := root.Execute(); e == nil {
			t.Fatalf("expected validation error for %v", args)
		}
	}
}
