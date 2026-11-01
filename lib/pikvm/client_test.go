package pikvm_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

func TestServiceRequests(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-KVMD-User") != "operator" || r.Header.Get("X-KVMD-Passwd") != "secret" {
			t.Error("missing authentication")
		}
		switch r.URL.Path {
		case "/api/hid/events/send_shortcut":
			if r.Method != "POST" || r.URL.Query().Get("keys") != "ControlLeft,KeyL" {
				t.Errorf("incorrect shortcut: %s %s", r.Method, r.URL)
			}
		case "/api/msd/set_params":
			if r.URL.Query().Get("image") != "https://host/image.iso?x=1&y=2" {
				t.Errorf("query was not preserved: %s", r.URL)
			}
		case "/api/hid/print":
			b, _ := io.ReadAll(r.Body)
			if string(b) != "hello\nworld" {
				t.Errorf("wrong text: %q", b)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	defer s.Close()
	c, err := pikvm.New(pikvm.Config{URL: s.URL, Username: "operator", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Shortcut(context.Background(), "ControlLeft", "KeyL"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.MSD().Params(context.Background(), url.Values{"image": {"https://host/image.iso?x=1&y=2"}}); err != nil {
		t.Fatal(err)
	}
	if err = c.TypeText(context.Background(), "hello\nworld", nil); err != nil {
		t.Fatal(err)
	}
}

func TestSessionAndBinaryTransfers(t *testing.T) {
	image := []byte{0xff, 0xd8, 0, 0x10, 0xff, 0xd9}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			_ = r.ParseForm()
			if r.Form.Get("passwd") != "secret" {
				t.Error("bad login form")
			}
			http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session", Path: "/"})
			return
		}
		cookie, e := r.Cookie("auth_token")
		if e != nil || cookie.Value != "session" {
			t.Error("missing session")
		}
		if r.URL.Path == "/api/msd/write" {
			b, _ := io.ReadAll(r.Body)
			if !bytes.Equal(b, image) || r.URL.Query().Get("image") != "test.iso" {
				t.Error("upload corrupted")
			}
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image)
	}))
	defer s.Close()
	c, _ := pikvm.New(pikvm.Config{URL: s.URL})
	if e := c.Login(context.Background(), "operator", "secret"); e != nil {
		t.Fatal(e)
	}
	if e := c.Upload(context.Background(), "test.iso", bytes.NewReader(image)); e != nil {
		t.Fatal(e)
	}
	var dst bytes.Buffer
	if e := c.Snapshot(context.Background(), nil, &dst); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(dst.Bytes(), image) {
		t.Fatal("snapshot corrupted")
	}
}
func TestHTTPErrorAndCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad secret", http.StatusForbidden) }))
	defer s.Close()
	c, _ := pikvm.New(pikvm.Config{URL: s.URL, Password: "secret"})
	_, err := c.Info(context.Background(), "")
	var apiErr *pikvm.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 403 || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected error %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Info(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
func TestOriginAndTLSProtection(t *testing.T) {
	for _, base := range []string{"", "ftp://pikvm", "https://admin:secret@pikvm", "https://pikvm?passwd=secret"} {
		if _, e := pikvm.New(pikvm.Config{URL: base}); e == nil {
			t.Errorf("accepted %q", base)
		}
	}
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect reached other origin") }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dest.URL, 302) }))
	defer source.Close()
	c, _ := pikvm.New(pikvm.Config{URL: source.URL, Username: "u", Password: "p"})
	if _, e := c.Info(context.Background(), ""); e == nil {
		t.Fatal("allowed cross-origin redirect")
	}
	for _, path := range []string{"https://example.com", "//example.com", "/api/info?password=x"} {
		if _, e := c.Request(context.Background(), "GET", path, nil, nil, ""); e == nil {
			t.Errorf("accepted %q", path)
		}
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"ok":true}`) }))
	defer tlsServer.Close()
	c, _ = pikvm.New(pikvm.Config{URL: tlsServer.URL})
	if _, e := c.Info(context.Background(), ""); e == nil {
		t.Error("trusted self-signed TLS by default")
	}
	c, _ = pikvm.New(pikvm.Config{URL: tlsServer.URL, Insecure: true})
	if _, e := c.Info(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
}
