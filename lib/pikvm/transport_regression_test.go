package pikvm_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

// readerOnly deliberately hides Len and Seek, as pipes and stdin do.
type readerOnly struct{ io.Reader }

func TestUploadFilesAndUnknownLengthReaders(t *testing.T) {
	for _, kind := range []string{"file", "unknown-length", "empty"} {
		t.Run(kind, func(t *testing.T) {
			temp := t.TempDir()
			t.Setenv("TMPDIR", temp)
			payload := "\x00\xffimage\n"
			var source io.Reader = readerOnly{strings.NewReader(payload)}
			if kind == "empty" {
				payload = ""
				source = readerOnly{strings.NewReader("")}
			}
			if kind == "file" {
				name := filepath.Join(t.TempDir(), "image.iso")
				if e := os.WriteFile(name, []byte("skip"+payload), 0600); e != nil {
					t.Fatal(e)
				}
				f, e := os.Open(name)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
				if _, e = f.Seek(4, io.SeekStart); e != nil {
					t.Fatal(e)
				}
				source = f
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ContentLength != int64(len(payload)) || len(r.TransferEncoding) != 0 {
					t.Errorf("upload requires exact Content-Length, got %d and %v", r.ContentLength, r.TransferEncoding)
				}
				data, _ := io.ReadAll(r.Body)
				if string(data) != payload {
					t.Errorf("upload bytes changed")
				}
				want := url.Values{"image": {"image.iso"}, "prefix": {"installers"}, "activate": {"false"}, "remove_incomplete": {"true"}}
				if !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("upload options differ")
				}
				_, _ = io.WriteString(w, `{"ok":true}`)
			}))
			defer server.Close()
			client, _ := pikvm.New(pikvm.Config{URL: server.URL})
			activate, remove := false, true
			resp, e := client.MSD().UploadWithOptions(context.Background(), pikvm.UploadOptions{Image: "image.iso", Prefix: "installers", Activate: &activate, RemoveIncomplete: &remove}, source)
			if e != nil {
				t.Fatal(e)
			}
			_ = resp.Body.Close()
			entries, e := os.ReadDir(temp)
			if e != nil {
				t.Fatal(e)
			}
			if len(entries) != 0 {
				t.Fatal("temporary upload was retained")
			}
		})
	}
}
func TestCanceledSpoolRemovesTemporaryFile(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	client, _ := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := client.Upload(ctx, "image.iso", readerOnly{strings.NewReader("image")})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", e)
	}
	entries, e := os.ReadDir(temp)
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 0 {
		t.Fatal("canceled upload left a temporary file")
	}
}
func TestLoginRedirectKeepsSessionAndStopsPasswordAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			if e := r.ParseForm(); e != nil {
				t.Error(e)
			}
			want := url.Values{"user": {"operator"}, "passwd": {"synthetic-otp-password"}, "expire": {"3600"}, "redirect": {"/kvm/"}}
			if !reflect.DeepEqual(r.PostForm, want) {
				t.Error("login options changed")
			}
			http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session", Path: "/"})
			http.Redirect(w, r, "/kvm/", http.StatusFound)
			return
		}
		if r.URL.Path != "/api/info" {
			t.Errorf("login redirect was followed")
		}
		if r.Header.Get("X-KVMD-User") != "" || r.Header.Get("X-KVMD-Passwd") != "" {
			t.Error("expired OTP credentials were resent after login")
		}
		cookie, e := r.Cookie("auth_token")
		if e != nil || cookie.Value != "session" {
			t.Error("session cookie missing")
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	client, e := pikvm.New(pikvm.Config{URL: server.URL, Username: "operator", Password: "synthetic-otp-password", HTTPClient: &http.Client{}})
	if e != nil {
		t.Fatal(e)
	}
	response, e := client.Auth().LoginWithOptions(context.Background(), pikvm.LoginOptions{User: "operator", Password: "synthetic-otp-password", Expire: "3600", Redirect: "/kvm/"})
	if e != nil {
		t.Fatal(e)
	}
	_ = response.Body.Close()
	if response.StatusCode != 302 {
		t.Fatal("redirect response not preserved")
	}
	if _, e = client.Info(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
}
func TestBasicAuthenticationAndPrivateErrorPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "operator" || password != "synthetic-password" || r.Header.Get("X-KVMD-User") != "" {
			t.Error("incorrect Basic authentication")
		}
		http.Error(w, "synthetic-private-payload", http.StatusForbidden)
	}))
	defer server.Close()
	client, _ := pikvm.New(pikvm.Config{URL: server.URL, Username: "operator", Password: "synthetic-password", AuthMode: "basic"})
	_, e := client.Info(context.Background(), "")
	var apiError *pikvm.APIError
	if !errors.As(e, &apiError) || apiError.StatusCode != 403 {
		t.Fatalf("expected status error, got %v", e)
	}
	if strings.Contains(e.Error(), "synthetic-private-payload") || strings.Contains(e.Error(), server.URL) {
		t.Error("public error exposed private details")
	}
	_, e = client.Dial(context.Background(), "/api/ws", nil)
	if !errors.As(e, &apiError) || strings.Contains(e.Error(), "synthetic-private-payload") {
		t.Error("WebSocket handshake error exposed payload")
	}
}
