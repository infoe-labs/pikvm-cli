//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

// This suite never sends HID input, changes power/storage, logs in/out, or starts video.
func TestLivePiKVMReadOnly(t *testing.T) {
	if os.Getenv("PIKVM_LIVE_TESTS") != "1" {
		t.Skip("set PIKVM_LIVE_TESTS=1 to opt in to live tests")
	}
	if os.Getenv("PIKVM_URL") == "" {
		t.Fatal("PIKVM_URL is required for live tests")
	}
	client, e := pikvm.New(pikvm.Config{URL: os.Getenv("PIKVM_URL"), Username: os.Getenv("PIKVM_USER"), Password: os.Getenv("PIKVM_PASSWORD"), Token: os.Getenv("PIKVM_TOKEN"), Insecure: os.Getenv("PIKVM_INSECURE") == "1", Timeout: 10 * time.Second})
	if e != nil {
		t.Fatal("invalid live device configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, ep := range pikvm.Endpoints() {
		if ep.Method != "GET" || ep.WebSocket || ep.StreamResponse || ep.Group == "video" || ep.Group == "media" || ep.Group == "redfish" || ep.Group == "alerts" || ep.Name == "inactivity" {
			continue
		}
		t.Run(ep.Group+"/"+ep.Name, func(t *testing.T) {
			resp, e := client.Request(ctx, ep.Method, ep.Path, nil, nil, "")
			if e != nil {
				var api *pikvm.APIError
				if errors.As(e, &api) && (api.StatusCode == 404 || api.StatusCode == 501) {
					t.Skip("endpoint not supported by device firmware")
				}
				t.Fatal("live read-only request failed")
			}
			defer resp.Body.Close()
			var result struct {
				OK bool `json:"ok"`
			}
			if json.NewDecoder(resp.Body).Decode(&result) != nil || !result.OK {
				t.Fatal("invalid live response envelope")
			}
		})
	}
	session, e := client.Events().Connect(ctx, url.Values{"stream": {"false"}})
	if e != nil {
		t.Fatal("live event connection failed")
	}
	defer session.Close()
	if session.Ping(ctx) != nil {
		t.Fatal("live ping failed")
	}
	for i := 0; i < 64; i++ {
		message, e := session.Read(ctx)
		if e != nil {
			t.Fatal("live event read failed")
		}
		var event struct {
			Type string `json:"event_type"`
		}
		_ = json.Unmarshal(message.Data, &event)
		if event.Type == "pong" {
			return
		}
	}
	t.Fatal("live pong was not received")
}
