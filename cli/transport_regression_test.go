package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/cli"
)

func TestLoginRedirectTokenAndAllFormFlags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Error("HTML redirect was followed")
		}
		_ = r.ParseForm()
		if r.PostForm.Get("expire") != "3600" || r.PostForm.Get("redirect") != "/kvm/" || r.PostForm.Get("user") != "form-user" || r.PostForm.Get("passwd") != "form-password" {
			t.Error("incorrect login form")
		}
		http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session", Path: "/"})
		http.Redirect(w, r, "/kvm/", 302)
	}))
	defer server.Close()
	root := cli.NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"--url", server.URL, "auth", "login", "--login-user", "form-user", "--login-password", "form-password", "--expire", "3600", "--redirect", "/kvm/"})
	if e := root.Execute(); e != nil {
		t.Fatal(e)
	}
	var result map[string]string
	if e := json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result["auth_token"] != "session" {
		t.Fatal("session token missing")
	}
}
func TestStdinUploadHasRequiredContentLength(t *testing.T) {
	payload := "\x00\xffiso"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.ContentLength != int64(len(payload)) || string(b) != payload || len(r.TransferEncoding) != 0 {
			t.Error("stdin upload was not sent with its exact length")
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	root := cli.NewRootCommand()
	root.SetIn(strings.NewReader(payload))
	root.SetOut(io.Discard)
	root.SetArgs([]string{"--url", server.URL, "msd", "upload", "--image", "image.iso", "--input", "-"})
	if e := root.Execute(); e != nil {
		t.Fatal(e)
	}
}
func TestMediaPingAndKeyframeCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opcode byte
	}{{"ping", 0}, {"keyframe", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/media/ws" {
					t.Error("wrong media endpoint")
				}
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
				if typ != websocket.MessageBinary || !bytes.Equal(data, []byte{tc.opcode}) {
					t.Error("wrong media opcode")
				}
				_ = s.Write(r.Context(), typ, data)
			}))
			defer server.Close()
			root := cli.NewRootCommand()
			root.SetOut(io.Discard)
			root.SetArgs([]string{"--url", server.URL, "media", tc.name, "--count", "1"})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := root.ExecuteContext(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
}
