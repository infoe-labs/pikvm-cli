package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

// fakeKVM is a synthetic PiKVM. It answers HEAD with 405 for registered paths and 404 for
// absent ones, mirroring kvmd so endpoint probing can be exercised without mutations.
type fakeKVM struct {
	mu          sync.Mutex
	authStatus  int
	brokenGET   []string
	infoBody    string
	hidBody     string
	gpioBody    string
	missingHEAD []string
	leds        map[string]bool
	pressStatus []int
	flipOnPress []bool
	presses     []string
	wsMode      string // ok, reject, silent
}

func (f *fakeKVM) ledJSON() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	leds := f.leds
	if leds == nil {
		leds = map[string]bool{"caps": false, "num": false, "scroll": false}
	}
	body, _ := json.Marshal(map[string]any{"ok": true, "result": map[string]any{
		"online":   true,
		"keyboard": map[string]any{"online": true, "outputs": map[string]any{"active": "usb", "available": []string{"usb"}}, "leds": leds},
		"mouse":    map[string]any{"online": true, "outputs": map[string]any{"active": "usb", "available": []string{"usb"}}, "absolute": true},
	}})
	return string(body)
}

func (f *fakeKVM) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		for _, path := range f.missingHEAD {
			if r.URL.Path == path {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	for _, path := range f.brokenGET {
		if r.URL.Path == path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}
	switch r.URL.Path {
	case "/api/auth/check":
		if f.authStatus != 0 && f.authStatus != http.StatusOK {
			w.WriteHeader(f.authStatus)
			return
		}
		writeJSON(w, `{"ok":true,"result":{}}`)
	case "/api/info":
		if f.infoBody == "" {
			f.infoBody = `{"ok":true,"result":{"system":{"kvmd":{"version":"4.20"},"streamer":{"app":"ustreamer","version":"6.15"}}}}`
		}
		writeJSON(w, f.infoBody)
	case "/api/hid":
		if f.hidBody == "" {
			writeJSON(w, f.ledJSON())
			return
		}
		writeJSON(w, f.hidBody)
	case "/api/gpio":
		if f.gpioBody == "" {
			f.gpioBody = `{"ok":true,"result":{"state":{"outputs":{"__v3_usb_breaker__":{"online":true,"state":true,"busy":false}}}}}`
		}
		writeJSON(w, f.gpioBody)
	case "/api/hid/events/send_key":
		f.handlePress(w, r)
	case "/api/ws":
		f.handleSocket(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeKVM) handlePress(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	f.mu.Lock()
	index := len(f.presses)
	f.presses = append(f.presses, key)
	status, flip := http.StatusOK, true
	if index < len(f.pressStatus) {
		status = f.pressStatus[index]
	} else if len(f.pressStatus) > 0 {
		status = f.pressStatus[len(f.pressStatus)-1]
	}
	if index < len(f.flipOnPress) {
		flip = f.flipOnPress[index]
	} else if len(f.flipOnPress) > 0 {
		flip = f.flipOnPress[len(f.flipOnPress)-1]
	}
	leds := f.leds
	if leds == nil {
		leds = map[string]bool{"caps": false, "num": false, "scroll": false}
	}
	if status == http.StatusOK && flip {
		switch key {
		case "NumLock":
			leds["num"] = !leds["num"]
		case "CapsLock":
			leds["caps"] = !leds["caps"]
		case "ScrollLock":
			leds["scroll"] = !leds["scroll"]
		}
	}
	f.leds = leds
	f.mu.Unlock()
	if status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, `{"ok":true,"result":{}}`)
}

func (f *fakeKVM) handleSocket(w http.ResponseWriter, r *http.Request) {
	if f.wsMode == "reject" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if f.wsMode == "silent" {
		<-r.Context().Done()
		return
	}
	_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"event_type":"info_fan_state","event":{}}`))
	<-r.Context().Done()
}

func (f *fakeKVM) pressKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.presses...)
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// withFastDoctor shrinks the polling bounds so tests do not sleep for real.
func withFastDoctor(t *testing.T) {
	t.Helper()
	interval, attempts, budget := doctorPollInterval, doctorPollAttempts, doctorSocketBudget
	doctorPollInterval, doctorPollAttempts, doctorSocketBudget = time.Millisecond, 3, 100*time.Millisecond
	t.Cleanup(func() {
		doctorPollInterval, doctorPollAttempts, doctorSocketBudget = interval, attempts, budget
	})
}

func doctorClient(t *testing.T, device *fakeKVM) *pikvm.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(device.serve))
	t.Cleanup(server.Close)
	client, err := pikvm.New(pikvm.Config{URL: server.URL, Username: "red", Password: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func statusOf(report doctorReport, name string) doctorCheck {
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	return doctorCheck{Name: name, Status: "absent"}
}

func TestDoctorHealthyDevice(t *testing.T) {
	withFastDoctor(t)
	client := doctorClient(t, &fakeKVM{})
	report := runDoctor(context.Background(), client, "https://pikvm.invalid", false)
	for name, want := range map[string]string{"auth": statusPass, "device": statusPass, "hid": statusPass, "usb-power": statusPass, "endpoints": statusPass, "websocket": statusPass, "input-roundtrip": statusSkip} {
		if got := statusOf(report, name); got.Status != want {
			t.Errorf("%s = %s (%s), want %s", name, got.Status, got.Detail, want)
		}
	}
	if report.failed() {
		t.Error("healthy device reported failure")
	}
	if !strings.Contains(statusOf(report, "device").Detail, "kvmd 4.20") {
		t.Errorf("device detail missing version: %s", statusOf(report, "device").Detail)
	}
	if !strings.Contains(statusOf(report, "input-roundtrip").Detail, "--test-input") {
		t.Error("skipped input check should explain how to enable it")
	}
}

func TestDoctorAuthAndDeviceFailures(t *testing.T) {
	withFastDoctor(t)
	report := runDoctor(context.Background(), doctorClient(t, &fakeKVM{authStatus: http.StatusUnauthorized}), "", false)
	if got := statusOf(report, "auth"); got.Status != statusFail {
		t.Errorf("unauthenticated device reported %s", got.Status)
	}
	if !report.failed() {
		t.Error("auth failure must fail the run")
	}

	report = runDoctor(context.Background(), doctorClient(t, &fakeKVM{infoBody: `{"ok":true,"result":{}}`}), "", false)
	if got := statusOf(report, "device"); got.Status != statusWarn {
		t.Errorf("missing version reported %s", got.Status)
	}

	report = runDoctor(context.Background(), doctorClient(t, &fakeKVM{brokenGET: []string{"/api/info"}}), "", false)
	if got := statusOf(report, "device"); got.Status != statusFail {
		t.Errorf("unreachable info reported %s", got.Status)
	}
}

func TestDoctorHIDVariants(t *testing.T) {
	withFastDoctor(t)
	for _, tc := range []struct {
		name, body, want string
	}{
		{"healthy", `{"ok":true,"result":{"online":true,"keyboard":{"online":true,"outputs":{"active":"usb"}},"mouse":{"online":true,"outputs":{"active":"usb"}}}}`, statusPass},
		{"no keyboard output", `{"ok":true,"result":{"online":true,"keyboard":{"online":true,"outputs":{"active":""}},"mouse":{"online":true,"outputs":{"active":"usb"}}}}`, statusWarn},
		{"keyboard offline", `{"ok":true,"result":{"online":true,"keyboard":{"online":false,"outputs":{"active":"usb"}},"mouse":{"online":true,"outputs":{"active":"usb"}}}}`, statusFail},
		{"broken", `{"ok":true,"result":{`, statusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := runDoctor(context.Background(), doctorClient(t, &fakeKVM{hidBody: tc.body}), "", false)
			if got := statusOf(report, "hid"); got.Status != tc.want {
				t.Errorf("hid = %s (%s), want %s", got.Status, got.Detail, tc.want)
			}
		})
	}
}

func TestDoctorUSBPowerVariants(t *testing.T) {
	withFastDoctor(t)
	for _, tc := range []struct {
		name, body, want string
	}{
		{"on", `{"ok":true,"result":{"state":{"outputs":{"__v3_usb_breaker__":{"online":true,"state":true}}}}}`, statusPass},
		{"off", `{"ok":true,"result":{"state":{"outputs":{"__v3_usb_breaker__":{"online":true,"state":false}}}}}`, statusFail},
		{"absent", `{"ok":true,"result":{"state":{"outputs":{"__v4_locator__":{"online":true,"state":false}}}}}`, statusSkip},
		{"broken", `nope`, statusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := runDoctor(context.Background(), doctorClient(t, &fakeKVM{gpioBody: tc.body}), "", false)
			if got := statusOf(report, "usb-power"); got.Status != tc.want {
				t.Errorf("usb-power = %s (%s), want %s", got.Status, got.Detail, tc.want)
			}
		})
	}
}

func TestDoctorEndpointParity(t *testing.T) {
	withFastDoctor(t)
	device := &fakeKVM{missingHEAD: []string{"/api/media", "/api/media/ws", "/api/alerts", "/redfish/v1/Managers/BMC", "/redfish/v1/Systems/1", "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset"}}
	report := runDoctor(context.Background(), doctorClient(t, device), "", false)
	check := statusOf(report, "endpoints")
	if check.Status != statusWarn {
		t.Fatalf("endpoints = %s, want %s", check.Status, statusWarn)
	}
	for _, want := range []string{"media/state", "media/connect", "alerts/state", "redfish/manager", "unconfirmed"} {
		if !strings.Contains(check.Detail, want) {
			t.Errorf("endpoint detail missing %q: %s", want, check.Detail)
		}
	}
	if !strings.Contains(check.Detail, "of") || !strings.Contains(check.Detail, "catalogued operations") {
		t.Errorf("endpoint detail lacks a count: %s", check.Detail)
	}
}

func TestDoctorEndpointTransportFailure(t *testing.T) {
	withFastDoctor(t)
	server := httptest.NewServer(http.HandlerFunc((&fakeKVM{}).serve))
	url := server.URL
	server.Close() // Probing now hits a dead origin.
	client, err := pikvm.New(pikvm.Config{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	var report doctorReport
	checkEndpoints(context.Background(), client, &report, pikvm.Endpoints())
	if got := statusOf(report, "endpoints"); got.Status != statusFail {
		t.Errorf("dead origin reported %s", got.Status)
	}
}

func TestDoctorEmptyCatalog(t *testing.T) {
	withFastDoctor(t)
	var report doctorReport
	checkEndpoints(context.Background(), doctorClient(t, &fakeKVM{}), &report, nil)
	if got := statusOf(report, "endpoints"); got.Status != statusWarn || !strings.Contains(got.Detail, "no catalogued") {
		t.Errorf("empty catalog reported %s (%s)", got.Status, got.Detail)
	}
}

func TestFetchResultEdgeCases(t *testing.T) {
	withFastDoctor(t)
	for _, tc := range []struct{ name, body, want string }{
		{"missing result", `{"ok":true}`, "no result"},
		{"wrong shape", `{"ok":true,"result":{"system":"not-an-object"}}`, "unexpected device response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := runDoctor(context.Background(), doctorClient(t, &fakeKVM{infoBody: tc.body}), "", false)
			check := statusOf(report, "device")
			if check.Status != statusFail || !strings.Contains(check.Detail, tc.want) {
				t.Errorf("device = %s (%s), want fail containing %q", check.Status, check.Detail, tc.want)
			}
		})
	}
}

func TestWaitForLEDCancelledContext(t *testing.T) {
	withFastDoctor(t)
	client := doctorClient(t, &fakeKVM{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitForLED(ctx, client, "num", true) {
		t.Error("a cancelled context must not report success")
	}
}

func TestDoctorJSONEncodeFailure(t *testing.T) {
	withFastDoctor(t)
	device := &fakeKVM{}
	server := httptest.NewServer(http.HandlerFunc(device.serve))
	defer server.Close()
	if err := executeRoot(failedWriter{}, io.Discard, "--url", server.URL, "doctor", "--json"); err == nil {
		t.Error("a failing writer must surface an error")
	}
}

func TestDoctorWebSocketVariants(t *testing.T) {
	withFastDoctor(t)
	for _, tc := range []struct {
		mode, want string
	}{{"ok", statusPass}, {"reject", statusFail}, {"silent", statusWarn}} {
		t.Run(tc.mode, func(t *testing.T) {
			report := runDoctor(context.Background(), doctorClient(t, &fakeKVM{wsMode: tc.mode}), "", false)
			if got := statusOf(report, "websocket"); got.Status != tc.want {
				t.Errorf("websocket = %s (%s), want %s", got.Status, got.Detail, tc.want)
			}
		})
	}
}

func TestDoctorInputRoundTrip(t *testing.T) {
	withFastDoctor(t)
	t.Run("verified and restored", func(t *testing.T) {
		device := &fakeKVM{wsMode: "reject"}
		report := runDoctor(context.Background(), doctorClient(t, device), "", true)
		check := statusOf(report, "input-roundtrip")
		if check.Status != statusPass {
			t.Fatalf("input = %s (%s), want pass", check.Status, check.Detail)
		}
		if !strings.Contains(check.Detail, "NumLock") {
			t.Errorf("expected NumLock to be used, got %s", check.Detail)
		}
		keys := device.pressKeys()
		if len(keys) != 2 || keys[0] != "NumLock" || keys[1] != "NumLock" {
			t.Errorf("expected a press and a restore, got %v", keys)
		}
	})
	t.Run("accepted but never echoed", func(t *testing.T) {
		device := &fakeKVM{flipOnPress: []bool{false}, wsMode: "reject"}
		report := runDoctor(context.Background(), doctorClient(t, device), "", true)
		if got := statusOf(report, "input-roundtrip"); got.Status != statusFail {
			t.Errorf("silent input must fail, got %s (%s)", got.Status, got.Detail)
		}
	})
	t.Run("keystroke rejected", func(t *testing.T) {
		device := &fakeKVM{pressStatus: []int{http.StatusNotFound}, wsMode: "reject"}
		report := runDoctor(context.Background(), doctorClient(t, device), "", true)
		if got := statusOf(report, "input-roundtrip"); got.Status != statusFail {
			t.Errorf("rejected keystroke must fail, got %s", got.Status)
		}
	})
	t.Run("restore request failed", func(t *testing.T) {
		device := &fakeKVM{pressStatus: []int{http.StatusOK, http.StatusInternalServerError}, wsMode: "reject"}
		report := runDoctor(context.Background(), doctorClient(t, device), "", true)
		if got := statusOf(report, "input-roundtrip"); got.Status != statusWarn {
			t.Errorf("failed restore must warn, got %s (%s)", got.Status, got.Detail)
		}
	})
	t.Run("target did not clear the key", func(t *testing.T) {
		device := &fakeKVM{flipOnPress: []bool{true, false}, wsMode: "reject"}
		report := runDoctor(context.Background(), doctorClient(t, device), "", true)
		check := statusOf(report, "input-roundtrip")
		if check.Status != statusWarn || !strings.Contains(check.Detail, "clear") {
			t.Errorf("uncleared key must warn with guidance, got %s (%s)", check.Status, check.Detail)
		}
	})
	t.Run("leds unreadable", func(t *testing.T) {
		device := &fakeKVM{hidBody: `broken`, wsMode: "reject"}
		report := runDoctor(context.Background(), doctorClient(t, device), "", true)
		if got := statusOf(report, "input-roundtrip"); got.Status != statusFail {
			t.Errorf("unreadable LEDs must fail, got %s", got.Status)
		}
	})
}

func TestDoctorUsesEveryLockKey(t *testing.T) {
	withFastDoctor(t)
	device := &fakeKVM{leds: map[string]bool{"caps": true, "num": true, "scroll": false}, wsMode: "reject"}
	if report := runDoctor(context.Background(), doctorClient(t, device), "", true); !strings.Contains(statusOf(report, "input-roundtrip").Detail, "ScrollLock") {
		t.Errorf("expected ScrollLock when the others are lit, got %s", statusOf(report, "input-roundtrip").Detail)
	}
	device = &fakeKVM{leds: map[string]bool{"caps": true, "num": true, "scroll": true}, wsMode: "reject"}
	if report := runDoctor(context.Background(), doctorClient(t, device), "", true); !strings.Contains(statusOf(report, "input-roundtrip").Detail, "NumLock") {
		t.Errorf("expected fallback to NumLock when all are lit, got %s", statusOf(report, "input-roundtrip").Detail)
	}
}

func TestPickLockKey(t *testing.T) {
	for _, tc := range []struct {
		leds   map[string]bool
		key    string
		expect bool
	}{
		{map[string]bool{"num": false, "caps": false, "scroll": false}, "NumLock", true},
		{map[string]bool{"num": true, "caps": false, "scroll": false}, "CapsLock", true},
		{map[string]bool{"num": true, "caps": true, "scroll": false}, "ScrollLock", true},
		{map[string]bool{"num": true, "caps": true, "scroll": true}, "NumLock", false},
		{nil, "NumLock", true},
	} {
		key, _, expect := pickLockKey(tc.leds)
		if key != tc.key || expect != tc.expect {
			t.Errorf("pickLockKey(%v) = %s/%t, want %s/%t", tc.leds, key, expect, tc.key, tc.expect)
		}
	}
}

func TestResolveDoctorPath(t *testing.T) {
	for _, tc := range []struct {
		in, want      string
		parameterized bool
	}{
		{"/api/hid", "/api/hid", false},
		{"/redfish/v1/Systems/{id}", "/redfish/v1/Systems/1", true},
		{"/api/msd/write", "/api/msd/write", false},
		{"/api/thing/{name}", "/api/thing/probe", true},
	} {
		got, parameterized := resolveDoctorPath(tc.in)
		if got != tc.want || parameterized != tc.parameterized {
			t.Errorf("resolveDoctorPath(%q) = %q/%t, want %q/%t", tc.in, got, parameterized, tc.want, tc.parameterized)
		}
	}
}

func TestLimitNamesAndDetail(t *testing.T) {
	limited := limitNames([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n"})
	if len(limited) != doctorListLimit+1 || !strings.Contains(limited[doctorListLimit], "2 more") {
		t.Errorf("long lists were not truncated: %v", limited)
	}
	if len(limitNames([]string{"a"})) != 1 {
		t.Error("short lists must be untouched")
	}
	long := fmt.Errorf("%s", strings.Repeat("x", doctorDetailLimit+50))
	if len(publicDetail(long)) != doctorDetailLimit {
		t.Error("detail was not bounded")
	}
}

func TestDoctorCommand(t *testing.T) {
	withFastDoctor(t)
	device := &fakeKVM{}
	server := httptest.NewServer(http.HandlerFunc(device.serve))
	defer server.Close()

	var out strings.Builder
	if err := executeRoot(&out, io.Discard, "--url", server.URL, "doctor"); err != nil {
		t.Fatalf("healthy doctor returned %v", err)
	}
	if !strings.Contains(out.String(), "PASS") || !strings.Contains(out.String(), "kvmd 4.20") {
		t.Errorf("unexpected text report:\n%s", out.String())
	}

	out.Reset()
	if err := executeRoot(&out, io.Discard, "--url", server.URL, "doctor", "--json"); err != nil {
		t.Fatal(err)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if report.Target != server.URL || len(report.Checks) == 0 {
		t.Errorf("unexpected JSON report %+v", report)
	}

	out.Reset()
	if err := executeRoot(&out, io.Discard, "--url", "https://pikvm.invalid:1", "doctor"); err == nil {
		t.Error("unreachable device must return an error")
	}

	out.Reset()
	if err := executeRoot(&out, io.Discard, "doctor"); err == nil {
		t.Error("doctor must require a URL")
	}
}
