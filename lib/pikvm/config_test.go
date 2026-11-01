package pikvm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTimeoutConfigurationAndRedirectPolicy(t *testing.T) {
	if _, e := New(Config{URL: "https://pikvm.example.com", AuthMode: "invalid"}); e == nil {
		t.Fatal("invalid auth mode accepted")
	}
	for _, timeout := range []time.Duration{0, -1, time.Second} {
		c, e := New(Config{URL: "https://pikvm.example.com", Timeout: timeout})
		if e != nil {
			t.Fatal(e)
		}
		want := timeout
		if timeout == 0 {
			want = 30 * time.Second
		}
		if timeout < 0 {
			want = 0
		}
		if c.http.Timeout != want {
			t.Error("timeout changed")
		}
	}
	sentinel := errors.New("synthetic redirect refusal")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/next", 302) }))
	defer server.Close()
	c, _ := New(Config{URL: server.URL, HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return sentinel }}})
	if _, e := c.Info(context.Background(), ""); !errors.Is(e, sentinel) {
		t.Fatal("custom redirect policy was lost")
	}
	c, _ = New(Config{URL: server.URL})
	if _, e := c.Info(context.Background(), ""); e == nil {
		t.Fatal("redirect loop was allowed")
	}
	if _, e := c.Request(context.Background(), "invalid\nmethod", "/api/info", nil, nil, ""); e == nil {
		t.Fatal("invalid HTTP method accepted")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestSpoolReadAndCreationFailures(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	_, _, done, e := uploadBody(context.Background(), failingReader{})
	done()
	if !errors.Is(e, io.ErrUnexpectedEOF) {
		t.Fatal("spool read error lost")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("failed spool retained data")
	}
	t.Setenv("TMPDIR", filepath.Join(dir, "missing"))
	_, _, done, e = uploadBody(context.Background(), strings.NewReader("body"))
	done()
	if e != nil {
		t.Error("known-length reader unexpectedly spooled")
	}
	_, _, done, e = uploadBody(context.Background(), failingReader{})
	done()
	if e == nil {
		t.Error("spool creation failure lost")
	}
}
