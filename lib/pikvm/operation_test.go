package pikvm_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

func TestSharedInvokerRejectsInvalidInputsBeforeTransport(t *testing.T) {
	c, _ := pikvm.New(pikvm.Config{URL: "https://pikvm.example.com", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid invocation reached transport")
		return nil, io.ErrUnexpectedEOF
	})}})
	for _, tc := range []struct {
		group, name string
		input       pikvm.OperationInput
	}{
		{"events", "connect", pikvm.OperationInput{}}, {"hid", "move", pikvm.OperationInput{}}, {"hid", "type", pikvm.OperationInput{}}, {"redfish", "insert", pikvm.OperationInput{}}, {"redfish", "insert", pikvm.OperationInput{Fields: map[string]any{"Image": "image.iso", "Inserted": "invalid"}}}, {"redfish", "insert", pikvm.OperationInput{Fields: map[string]any{"Image": "image.iso"}, Body: strings.NewReader("{}")}}, {"auth", "login", pikvm.OperationInput{Fields: map[string]any{"user": true}}},
	} {
		for _, ep := range pikvm.Endpoints() {
			if ep.Group == tc.group && ep.Name == tc.name {
				if _, e := c.Invoke(context.Background(), ep, tc.input); e == nil {
					t.Errorf("invalid %s/%s invocation accepted", tc.group, tc.name)
				}
			}
		}
	}
}
