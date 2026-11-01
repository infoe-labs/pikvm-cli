package pikvm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func callService(t *testing.T, c *pikvm.Client, ep pikvm.Endpoint, id string) []reflect.Value {
	t.Helper()
	names := map[string]string{"hid": "HID", "atx": "ATX", "msd": "MSD", "gpio": "GPIO"}
	name := names[ep.Group]
	if name == "" {
		name = strings.ToUpper(ep.Group[:1]) + ep.Group[1:]
	}
	service := reflect.ValueOf(c).MethodByName(name).Call(nil)[0]
	method := ""
	for _, word := range strings.Split(ep.Name, "-") {
		method += strings.ToUpper(word[:1]) + word[1:]
	}
	args := []reflect.Value{reflect.ValueOf(context.Background())}
	if strings.Contains(ep.Path, "{id}") {
		args = append(args, reflect.ValueOf(id))
	}
	args = append(args, reflect.ValueOf(url.Values{}))
	if ep.ContentType != "" {
		args = append(args, reflect.ValueOf(strings.NewReader("synthetic body")))
	}
	return service.MethodByName(method).Call(args)
}
func TestEveryServicePropagatesHTTPAndTransportErrors(t *testing.T) {
	sentinel := errors.New("synthetic transport failure")
	for _, status := range []int{403, 503, 0} {
		for _, ep := range pikvm.Endpoints() {
			t.Run(fmt.Sprintf("%d/%s/%s", status, ep.Group, ep.Name), func(t *testing.T) {
				transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if status == 0 {
						return nil, sentinel
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("synthetic-private-response")), Request: r}, nil
				})
				client, e := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com", HTTPClient: &http.Client{Transport: transport}})
				if e != nil {
					t.Fatal(e)
				}
				result := callService(t, client, ep, "0")
				if result[1].IsNil() {
					t.Fatal("service swallowed failure")
				}
				err := result[1].Interface().(error)
				if status == 0 {
					if !errors.Is(err, sentinel) {
						t.Fatal("lost transport error cause")
					}
				} else {
					var api *pikvm.APIError
					if !errors.As(err, &api) || api.StatusCode != status {
						t.Fatal("lost HTTP status")
					}
				}
				if strings.Contains(err.Error(), "synthetic-private-response") {
					t.Fatal("failure exposed response payload")
				}
			})
		}
	}
}
func TestRedfishInvalidIDsFailBeforeNetwork(t *testing.T) {
	c, _ := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Error("invalid id reached transport")
		return nil, errors.New("unexpected network")
	})}})
	for _, ep := range pikvm.Endpoints() {
		if strings.Contains(ep.Path, "{id}") {
			for _, id := range []string{"", "/", "?", "#"} {
				if callService(t, c, ep, id)[1].IsNil() {
					t.Fatal("accepted invalid id")
				}
			}
		}
	}
}
func TestTypedRedfishBodiesPreserveFalseAndOmission(t *testing.T) {
	no := false
	for _, tc := range []struct {
		name    string
		request pikvm.InsertMediaRequest
		want    string
	}{
		{"omitted", pikvm.InsertMediaRequest{Image: "image.iso"}, `{"Image":"image.iso"}`},
		{"explicit false", pikvm.InsertMediaRequest{Image: "image.iso", Inserted: &no, WriteProtected: &no, UserName: "media-user", Password: "synthetic-password", VerifyCertificate: &no, Oem: &pikvm.MediaOEM{PiKVM: pikvm.MediaPiKVM{DriveOptical: &no}}}, `{"Image":"image.iso","Inserted":false,"WriteProtected":false,"UserName":"media-user","Password":"synthetic-password","VerifyCertificate":false,"Oem":{"PiKVM":{"DriveOptical":false}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				data, _ := io.ReadAll(r.Body)
				var got, want any
				_ = json.Unmarshal(data, &got)
				_ = json.Unmarshal([]byte(tc.want), &want)
				if !reflect.DeepEqual(got, want) {
					t.Error("typed media fields changed")
				}
				return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})}})
			r, e := c.Redfish().InsertMedia(context.Background(), tc.request)
			if e != nil {
				t.Fatal(e)
			}
			_ = r.Body.Close()
		})
	}
	c, _ := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != `{"ResetType":"On"}` || r.URL.Path != "/redfish/v1/Systems/2/Actions/ComputerSystem.Reset" {
			t.Error("reset request changed")
		}
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: r}, nil
	})}})
	r, e := c.Redfish().ResetSystem(context.Background(), "2", pikvm.ResetRequest{ResetType: "On"})
	if e != nil {
		t.Fatal(e)
	}
	_ = r.Body.Close()
}
func TestPowerValidationAndQueryFlags(t *testing.T) {
	for _, action := range []string{"on", "off", "off_hard", "reset_hard"} {
		c, _ := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Query().Get("action") != action || r.URL.Query().Get("wait") != "false" {
				t.Error("power options changed")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
		})}})
		if e := c.Power(context.Background(), action, false); e != nil {
			t.Fatal(e)
		}
		if e := c.Power(context.Background(), "invalid", false); e == nil {
			t.Fatal("invalid action accepted")
		}
	}
	for _, ep := range pikvm.Endpoints() {
		for _, parameter := range ep.Parameters {
			flag := ep.QueryFlag(parameter)
			if flag == "" || strings.ContainsAny(flag, "_*") {
				t.Errorf("invalid flag %q", flag)
			}
		}
	}
}
