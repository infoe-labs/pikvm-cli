package pikvm_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

func TestWebSocketValidationCancellationAndReadLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer s.CloseNow()
		_, _, e = s.Read(r.Context())
		if e == nil {
			_ = s.Write(r.Context(), websocket.MessageText, []byte(strings.Repeat("x", 1024)))
		}
	}))
	defer server.Close()
	c, _ := pikvm.New(pikvm.Config{URL: server.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, e := c.Dial(ctx, "/api/ws", nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, e := range []error{s.SendEvent(ctx, "", nil), s.SendEvent(ctx, "invalid", make(chan int)), s.SendJSON(ctx, []byte("{")), s.SendBinary(ctx, nil)} {
		if e == nil {
			t.Fatal("malformed outgoing event accepted")
		}
	}
	s.SetReadLimit(32)
	if e = s.SendJSON(ctx, []byte(`{"event_type":"ping","event":{}}`)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Read(ctx); e == nil {
		t.Fatal("read limit was not enforced")
	}
	_ = s.Close()
	if e = s.SendBinary(ctx, []byte{0}); e == nil {
		t.Fatal("write to closed session succeeded")
	}
	if _, e = c.Dial(ctx, "https://other.example.com", nil); e == nil {
		t.Fatal("invalid WebSocket path accepted")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, e = c.Dial(canceled, "/api/ws", nil); !errors.Is(e, context.Canceled) {
		t.Fatal("WebSocket cancellation lost")
	}
}
func TestMediaKeyframeConvenience(t *testing.T) {
	server := echoSocketServer(t, "/api/media/ws")
	defer server.Close()
	c, _ := pikvm.New(pikvm.Config{URL: server.URL, Username: "user", Password: "password"})
	s, e := c.Media().Connect(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.RequestKeyframe(context.Background()); e != nil {
		t.Fatal(e)
	}
	m, e := s.Read(context.Background())
	if e != nil || len(m.Data) != 1 || m.Data[0] != 1 {
		t.Fatal("keyframe opcode changed")
	}
}
