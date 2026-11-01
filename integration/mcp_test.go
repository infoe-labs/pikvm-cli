package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPStdioBinaryAndGoRunEntrypoint(t *testing.T) {
	for _, source := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed binary", true: "go run main.go"}[source], func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/info" || r.Header.Get("X-KVMD-User") != "operator" {
					t.Error("MCP did not reuse configured client")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ok":true,"result":{"model":"synthetic"}}`)
			}))
			defer backend.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.Command(binary, "mcp")
			if source {
				cmd = exec.Command("go", "run", "../main.go", "mcp")
			}
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "PIKVM_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "PIKVM_URL="+backend.URL, "PIKVM_USER=operator", "PIKVM_PASSWORD=synthetic-mcp-password")
			session, e := sdk.NewClient(&sdk.Implementation{Name: "integration-test", Version: "test"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
			if e != nil {
				t.Fatal("MCP stdio initialization failed")
			}
			defer session.Close()
			tools, e := session.ListTools(ctx, nil)
			if e != nil || len(tools.Tools) != len(pikvm.Endpoints()) {
				t.Fatal("MCP tool discovery failed")
			}
			encoded, _ := json.Marshal(tools)
			if strings.Contains(string(encoded), "synthetic-mcp-password") || strings.Contains(string(encoded), backend.URL) {
				t.Fatal("MCP schemas exposed connection configuration")
			}
			result, e := session.CallTool(ctx, &sdk.CallToolParams{Name: "pikvm_system_info", Arguments: map[string]any{"fields": "hw"}})
			if e != nil || result.IsError {
				t.Fatal("MCP stdio tool call failed")
			}
		})
	}
}
