package pikvm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// LoginOptions exposes every login form parameter. Redirect responses are returned
// without following the redirect, so the caller can inspect the session cookie.
type LoginOptions struct{ User, Password, Expire, Redirect string }

func (s *AuthService) LoginWithOptions(ctx context.Context, options LoginOptions) (*http.Response, error) {
	form := url.Values{"user": {options.User}, "passwd": {options.Password}}
	if options.Expire != "" {
		form.Set("expire", options.Expire)
	}
	if options.Redirect != "" {
		form.Set("redirect", options.Redirect)
	}
	return s.Login(ctx, nil, strings.NewReader(form.Encode()))
}

// ResetRequest is the Redfish ComputerSystem.Reset body.
type ResetRequest struct {
	ResetType string `json:"ResetType"`
}

// InsertMediaRequest exposes all PiKVM Redfish media fields. Pointer booleans
// distinguish an omitted field from an explicit false.
type InsertMediaRequest struct {
	Image             string    `json:"Image"`
	Inserted          *bool     `json:"Inserted,omitempty"`
	WriteProtected    *bool     `json:"WriteProtected,omitempty"`
	UserName          string    `json:"UserName,omitempty"`
	Password          string    `json:"Password,omitempty"`
	VerifyCertificate *bool     `json:"VerifyCertificate,omitempty"`
	Oem               *MediaOEM `json:"Oem,omitempty"`
}
type MediaOEM struct {
	PiKVM MediaPiKVM `json:"PiKVM"`
}
type MediaPiKVM struct {
	DriveOptical *bool `json:"DriveOptical,omitempty"`
}

// ResetSystem sends the typed Redfish reset request. Close the returned response body.
func (s *RedfishService) ResetSystem(ctx context.Context, id string, request ResetRequest) (*http.Response, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return s.Power(ctx, id, nil, strings.NewReader(string(data)))
}

// InsertMedia sends the typed Redfish media request. Close the returned response body.
func (s *RedfishService) InsertMedia(ctx context.Context, request InsertMediaRequest) (*http.Response, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return s.Insert(ctx, nil, strings.NewReader(string(data)))
}

// UploadOptions covers the query parameters for a local MSD upload.
type UploadOptions struct {
	Image, Prefix              string
	Activate, RemoveIncomplete *bool
}

func (s *MSDService) UploadWithOptions(ctx context.Context, options UploadOptions, body io.Reader) (*http.Response, error) {
	query := url.Values{"image": {options.Image}}
	if options.Prefix != "" {
		query.Set("prefix", options.Prefix)
	}
	if options.Activate != nil {
		query.Set("activate", strconv.FormatBool(*options.Activate))
	}
	if options.RemoveIncomplete != nil {
		query.Set("remove_incomplete", strconv.FormatBool(*options.RemoveIncomplete))
	}
	return s.Upload(ctx, query, body)
}

// Delta is a relative mouse movement or wheel step. A list retains every step.
type Delta struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func (s *Session) Ping(ctx context.Context) error { return s.SendEvent(ctx, "ping", nil) }
func (s *Session) Key(ctx context.Context, key string, state, finish bool) error {
	return s.SendEvent(ctx, "key", map[string]any{"key": key, "state": state, "finish": finish})
}
func (s *Session) MouseButton(ctx context.Context, button string, state bool) error {
	return s.SendEvent(ctx, "mouse_button", map[string]any{"button": button, "state": state})
}
func (s *Session) MouseMove(ctx context.Context, x, y int) error {
	return s.SendEvent(ctx, "mouse_move", map[string]any{"to": Delta{x, y}})
}
func (s *Session) MouseRelative(ctx context.Context, deltas []Delta, squash bool) error {
	return s.SendEvent(ctx, "mouse_relative", map[string]any{"delta": deltas, "squash": squash})
}
func (s *Session) MouseWheel(ctx context.Context, deltas []Delta, squash bool) error {
	return s.SendEvent(ctx, "mouse_wheel", map[string]any{"delta": deltas, "squash": squash})
}
func (s *Session) StartMedia(ctx context.Context, mediaType, format string) error {
	return s.SendEvent(ctx, "start", map[string]string{"type": mediaType, "format": format})
}
func (s *Session) RequestKeyframe(ctx context.Context) error { return s.SendBinary(ctx, []byte{1}) }
