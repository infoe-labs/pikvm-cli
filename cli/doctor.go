package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

// Health check statuses reported by doctor.
const (
	statusPass = "pass"
	statusWarn = "warn"
	statusFail = "fail"
	statusSkip = "skip"
)

// Bounds for the opt-in HID round-trip. They are variables so tests stay fast.
var (
	doctorPollInterval = 250 * time.Millisecond
	doctorPollAttempts = 8
	doctorSocketBudget = 3 * time.Second
	doctorDetailLimit  = 160
	doctorListLimit    = 12
)

// doctorCheck is one named result. Details never carry credentials or payloads.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// doctorReport is the ordered result of a health run against one device.
type doctorReport struct {
	Target string        `json:"target"`
	Checks []doctorCheck `json:"checks"`
}

func (r *doctorReport) add(name, status, detail string) {
	r.Checks = append(r.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
}

func (r doctorReport) failed() bool {
	for _, check := range r.Checks {
		if check.Status == statusFail {
			return true
		}
	}
	return false
}

// newDoctorCommand builds the read-only health check command. Nothing here mutates the
// device: probes are HEAD or read-only GET, and the single keystroke path is opt-in.
func newDoctorCommand(cfg *pikvm.Config) *cobra.Command {
	var asJSON, testInput bool
	cmd := &cobra.Command{Use: "doctor", Short: "Run read-only health checks against the configured device", Args: cobra.NoArgs, Long: "Check connectivity, authentication, device identity, HID readiness, USB power,\nendpoint availability and the event stream. All probes are read-only. Pass\n--test-input to additionally type and clear one lock key on the target host.", RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newClient(*cfg)
		if err != nil {
			return err
		}
		report := runDoctor(cmd.Context(), client, cfg.URL, testInput)
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(report); err != nil {
				return err
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "target: %s\n", report.Target)
			for _, check := range report.Checks {
				fmt.Fprintf(cmd.OutOrStdout(), "%-4s %-18s %s\n", strings.ToUpper(check.Status), check.Name, check.Detail)
			}
		}
		if report.failed() {
			return fmt.Errorf("health check failed")
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit machine-readable JSON")
	cmd.Flags().BoolVar(&testInput, "test-input", false, "Opt in to a keystroke round-trip: type and clear one lock key on the target host")
	return cmd
}

// runDoctor executes every check. A check never aborts its siblings.
func runDoctor(ctx context.Context, client *pikvm.Client, target string, testInput bool) doctorReport {
	report := doctorReport{Target: target}
	checkAuth(ctx, client, &report)
	checkDevice(ctx, client, &report)
	checkHID(ctx, client, &report)
	checkUSBPower(ctx, client, &report)
	checkEndpoints(ctx, client, &report, pikvm.Endpoints())
	checkSocket(ctx, client, &report)
	if testInput {
		checkInput(ctx, client, &report)
	} else {
		report.add("input-roundtrip", statusSkip, "not requested; pass --test-input")
	}
	return report
}

func publicDetail(err error) string {
	detail := ErrorMessage(err)
	if doctorDetailLimit > 0 && len(detail) > doctorDetailLimit {
		detail = detail[:doctorDetailLimit]
	}
	return detail
}

// fetchResult decodes the data inside the PiKVM ok/result envelope.
func fetchResult(ctx context.Context, client *pikvm.Client, path string, result any) error {
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := client.JSON(ctx, "GET", path, nil, &envelope); err != nil {
		return err
	}
	if len(envelope.Result) == 0 {
		return fmt.Errorf("device returned no result")
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("unexpected device response: %w", err)
	}
	return nil
}

func checkAuth(ctx context.Context, client *pikvm.Client, report *doctorReport) {
	if err := client.JSON(ctx, "GET", "/api/auth/check", nil, nil); err != nil {
		report.add("auth", statusFail, publicDetail(err))
		return
	}
	report.add("auth", statusPass, "device reachable and credentials accepted")
}

func checkDevice(ctx context.Context, client *pikvm.Client, report *doctorReport) {
	var info struct {
		System struct {
			KVMD struct {
				Version string `json:"version"`
			} `json:"kvmd"`
			Streamer struct {
				App     string `json:"app"`
				Version string `json:"version"`
			} `json:"streamer"`
		} `json:"system"`
	}
	if err := fetchResult(ctx, client, "/api/info", &info); err != nil {
		report.add("device", statusFail, publicDetail(err))
		return
	}
	kvmd := info.System.KVMD.Version
	if kvmd == "" {
		report.add("device", statusWarn, "connected but the kvmd version is not reported")
		return
	}
	detail := "kvmd " + kvmd
	if app := info.System.Streamer.App; app != "" {
		detail += ", " + app
		if version := info.System.Streamer.Version; version != "" {
			detail += " " + version
		}
	}
	report.add("device", statusPass, detail)
}

func checkHID(ctx context.Context, client *pikvm.Client, report *doctorReport) {
	var hid struct {
		Online   bool `json:"online"`
		Keyboard struct {
			Online  bool `json:"online"`
			Outputs struct {
				Active string `json:"active"`
			} `json:"outputs"`
		} `json:"keyboard"`
		Mouse struct {
			Online  bool `json:"online"`
			Outputs struct {
				Active string `json:"active"`
			} `json:"outputs"`
		} `json:"mouse"`
	}
	if err := fetchResult(ctx, client, "/api/hid", &hid); err != nil {
		report.add("hid", statusFail, publicDetail(err))
		return
	}
	if !hid.Online || !hid.Keyboard.Online {
		report.add("hid", statusFail, fmt.Sprintf("keyboard online=%t mouse online=%t; the target cannot receive input", hid.Keyboard.Online, hid.Mouse.Online))
		return
	}
	detail := fmt.Sprintf("keyboard and mouse online (keyboard output %q, mouse output %q)", hid.Keyboard.Outputs.Active, hid.Mouse.Outputs.Active)
	if hid.Keyboard.Outputs.Active == "" {
		report.add("hid", statusWarn, detail+" ; no active keyboard output channel, keystrokes may never reach the target")
		return
	}
	report.add("hid", statusPass, detail)
}

func checkUSBPower(ctx context.Context, client *pikvm.Client, report *doctorReport) {
	var gpio struct {
		State struct {
			Outputs map[string]struct {
				State bool `json:"state"`
			} `json:"outputs"`
		} `json:"state"`
	}
	if err := fetchResult(ctx, client, "/api/gpio", &gpio); err != nil {
		report.add("usb-power", statusFail, publicDetail(err))
		return
	}
	names := make([]string, 0, len(gpio.State.Outputs))
	for name := range gpio.State.Outputs {
		if strings.Contains(name, "usb_breaker") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		report.add("usb-power", statusSkip, "no USB breaker GPIO is configured")
		return
	}
	sort.Strings(names)
	for _, name := range names {
		if !gpio.State.Outputs[name].State {
			report.add("usb-power", statusFail, "USB power to the target is off; the target cannot see the keyboard")
			return
		}
	}
	report.add("usb-power", statusPass, fmt.Sprintf("USB power on for %d breaker output(s)", len(names)))
}

// resolveDoctorPath substitutes path placeholders so a probe still matches its route.
// The second result reports whether the path was parameterized.
func resolveDoctorPath(path string) (string, bool) {
	if !strings.Contains(path, "{") {
		return path, false
	}
	return doctorPlaceholder.ReplaceAllStringFunc(path, func(match string) string {
		if strings.Trim(match, "{}") == "id" {
			return "1"
		}
		return "probe"
	}), true
}

var doctorPlaceholder = regexp.MustCompile(`\{[A-Za-z_]+\}`)

// checkEndpoints probes every catalogued operation with HEAD. The device answers 405 for
// registered paths and 404 for absent ones, so existence is confirmed without ever
// executing a mutation such as attaching media or switching a KVM port.
func checkEndpoints(ctx context.Context, client *pikvm.Client, report *doctorReport, endpoints []pikvm.Endpoint) {
	var missing, unconfirmed []string
	probed := 0
	for _, endpoint := range endpoints {
		path, parameterized := resolveDoctorPath(endpoint.Path)
		response, err := client.Request(ctx, http.MethodHead, path, nil, nil, "")
		status := 0
		if err != nil {
			var apiErr *pikvm.APIError
			if !errors.As(err, &apiErr) {
				report.add("endpoints", statusFail, "probing stopped: "+publicDetail(err))
				return
			}
			status = apiErr.StatusCode
		} else {
			status = response.StatusCode
			response.Body.Close()
		}
		probed++
		if status != http.StatusNotFound {
			continue
		}
		name := endpoint.Group + "/" + endpoint.Name
		if parameterized {
			unconfirmed = append(unconfirmed, name)
		} else {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unconfirmed)
	if probed == 0 {
		report.add("endpoints", statusWarn, "no catalogued endpoints were probed")
		return
	}
	detail := fmt.Sprintf("%d of %d catalogued operations are available", probed-len(missing), probed)
	if len(missing) == 0 {
		report.add("endpoints", statusPass, detail)
		return
	}
	detail += "; not supported by this firmware: " + strings.Join(limitNames(missing), ", ")
	if len(unconfirmed) > 0 {
		detail += "; unconfirmed (parameterized path): " + strings.Join(limitNames(unconfirmed), ", ")
	}
	report.add("endpoints", statusWarn, detail)
}

func limitNames(names []string) []string {
	if len(names) <= doctorListLimit {
		return names
	}
	return append(names[:doctorListLimit:doctorListLimit], fmt.Sprintf("(and %d more)", len(names)-doctorListLimit))
}

func checkSocket(ctx context.Context, client *pikvm.Client, report *doctorReport) {
	session, err := client.Dial(ctx, "/api/ws", nil)
	if err != nil {
		report.add("websocket", statusFail, publicDetail(err))
		return
	}
	defer session.Close()
	readCtx, cancel := context.WithTimeout(ctx, doctorSocketBudget)
	defer cancel()
	if _, err := session.Read(readCtx); err != nil {
		report.add("websocket", statusWarn, "connected but no event arrived: "+publicDetail(err))
		return
	}
	report.add("websocket", statusPass, "event stream is live")
}

// lockKeys are safe toggles: they are inert on a normal desktop yet their LED state is
// set by the target operating system, so an echo proves input actually arrived.
var lockKeys = []struct{ key, field string }{{"NumLock", "num"}, {"CapsLock", "caps"}, {"ScrollLock", "scroll"}}

func readLEDs(ctx context.Context, client *pikvm.Client) (map[string]bool, error) {
	var hid struct {
		Keyboard struct {
			LEDs map[string]bool `json:"leds"`
		} `json:"keyboard"`
	}
	if err := fetchResult(ctx, client, "/api/hid", &hid); err != nil {
		return nil, err
	}
	return hid.Keyboard.LEDs, nil
}

func pressLockKey(ctx context.Context, client *pikvm.Client, key string) error {
	query := url.Values{"key": {key}, "state": {"true"}, "finish": {"true"}}
	response, err := client.Request(ctx, "POST", "/api/hid/events/send_key", query, nil, "")
	if err != nil {
		return err
	}
	return response.Body.Close()
}

// pickLockKey prefers a key that is currently off so the first press must change state.
func pickLockKey(leds map[string]bool) (key, field string, expect bool) {
	for _, candidate := range lockKeys {
		if !leds[candidate.field] {
			return candidate.key, candidate.field, true
		}
	}
	return lockKeys[0].key, lockKeys[0].field, false
}

func waitForLED(ctx context.Context, client *pikvm.Client, field string, want bool) bool {
	for attempt := 0; attempt < doctorPollAttempts; attempt++ {
		if leds, err := readLEDs(ctx, client); err == nil && leds[field] == want {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(doctorPollInterval):
		}
	}
	return false
}

// checkInput verifies the one thing every other check cannot: that a keystroke accepted by
// the device is actually delivered to and acknowledged by the target host.
func checkInput(ctx context.Context, client *pikvm.Client, report *doctorReport) {
	leds, err := readLEDs(ctx, client)
	if err != nil {
		report.add("input-roundtrip", statusFail, publicDetail(err))
		return
	}
	key, field, expect := pickLockKey(leds)
	if err := pressLockKey(ctx, client, key); err != nil {
		report.add("input-roundtrip", statusFail, "cannot send a test keystroke: "+publicDetail(err))
		return
	}
	if !waitForLED(ctx, client, field, expect) {
		report.add("input-roundtrip", statusFail, key+" was accepted by the device but the target never echoed the LED; input is not reaching the host")
		return
	}
	if err := pressLockKey(ctx, client, key); err != nil {
		report.add("input-roundtrip", statusWarn, key+" verified but it could not be cleared: "+publicDetail(err))
		return
	}
	if !waitForLED(ctx, client, field, !expect) {
		report.add("input-roundtrip", statusWarn, key+" verified but the target did not clear it; clear it physically")
		return
	}
	report.add("input-roundtrip", statusPass, key+" round-trip confirmed by the target and restored")
}
