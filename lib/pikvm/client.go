// Package pikvm provides a context-aware client for the PiKVM HTTP and WebSocket APIs.
package pikvm

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config configures a client. TLS verification is enabled unless Insecure is set.
// A zero Timeout defaults to 30 seconds; a negative Timeout disables the timeout.
// HTTPClient, when supplied, controls transport and timeout instead.
type Config struct {
	URL, Username, Password, Token string
	Insecure                       bool
	Timeout                        time.Duration
	HTTPClient                     *http.Client
	// AuthMode is headers (default) or basic. Token and session cookies take priority.
	AuthMode string
}

type Client struct {
	base                      *url.URL
	http                      *http.Client
	username, password, token string
	authMode                  string
}

// APIError describes a failed HTTP request without exposing request credentials.
type APIError struct {
	StatusCode            int
	Method, Path, Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("PiKVM %s %s: HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

func New(cfg Config) (*Client, error) {
	if cfg.AuthMode != "" && cfg.AuthMode != "headers" && cfg.AuthMode != "basic" {
		return nil, fmt.Errorf("auth mode must be headers or basic")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("URL must be an http(s) base URL without credentials, query, or fragment")
	}
	h := cfg.HTTPClient
	if h == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.Insecure} // Explicit opt-in for self-signed devices.
		timeout := cfg.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		if timeout < 0 {
			timeout = 0
		}
		jar, _ := cookiejar.New(nil)
		h = &http.Client{Transport: transport, Timeout: timeout, Jar: jar}
	}
	// Credentials must never follow redirects to another host (or to plaintext HTTP).
	copyClient := *h
	if copyClient.Jar == nil {
		copyClient.Jar, _ = cookiejar.New(nil)
	}
	previous := h.CheckRedirect
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != u.Host || req.URL.Scheme != u.Scheme {
			return fmt.Errorf("refusing redirect outside PiKVM origin")
		}
		if len(via) > 0 && strings.HasSuffix(via[0].URL.Path, "/api/auth/login") {
			return http.ErrUseLastResponse
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
	return &Client{base: u, http: &copyClient, username: cfg.Username, password: cfg.Password, token: cfg.Token, authMode: cfg.AuthMode}, nil
}

// Request returns a successful response; the caller must close its body.
// path must be an absolute API path, never a URL. Bodies are streamed.
func (c *Client) Request(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	u, err := c.resolveURL(path, query)
	if err != nil {
		return nil, err
	}
	var length int64 = -1
	cleanup := func() {}
	if method == "POST" && path == "/api/msd/write" {
		body, length, cleanup, err = uploadBody(ctx, body)
		if err != nil {
			return nil, err
		}
		defer cleanup()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if length >= 0 {
		req.ContentLength = length
		if length == 0 {
			req.Body = http.NoBody
		}
	}
	c.authenticate(req)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PiKVM %s %s request failed: %w", method, path, safeTransportError(err))
	}
	if (resp.StatusCode < 200 || resp.StatusCode >= 300) && !(path == "/api/auth/login" && resp.StatusCode == http.StatusFound) {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		message := http.StatusText(resp.StatusCode)
		return nil, &APIError{resp.StatusCode, method, path, message}
	}
	return resp, nil
}

// url.Error includes the complete URL, which may contain remote-media passwords.
// TransportError retains the underlying error for errors.Is/As without printing
// hostnames, URLs, credentials, or other transport-specific details.
type TransportError struct{ cause error }

func (e *TransportError) Error() string { return "network or TLS request failed" }
func (e *TransportError) Unwrap() error { return e.cause }
func safeTransportError(err error) error {
	if e, ok := err.(*url.Error); ok {
		err = e.Err
	}
	return &TransportError{cause: err}
}

// JSON decodes the full PiKVM response envelope (ok/result) into out.
func (c *Client) JSON(ctx context.Context, method, path string, q url.Values, out any) error {
	resp, err := c.Request(ctx, method, path, q, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Login stores a session cookie in the client's cookie jar.
func (c *Client) Login(ctx context.Context, user, password string) error {
	form := url.Values{"user": {user}, "passwd": {password}}
	resp, err := c.Request(ctx, "POST", "/api/auth/login", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}
func (c *Client) Info(ctx context.Context, fields string) (json.RawMessage, error) {
	var result json.RawMessage
	q := url.Values{}
	if fields != "" {
		q.Set("fields", fields)
	}
	err := c.JSON(ctx, "GET", "/api/info", q, &result)
	return result, err
}
func (c *Client) TypeText(ctx context.Context, text string, options url.Values) error {
	r, e := c.Request(ctx, "POST", "/api/hid/print", options, strings.NewReader(text), "text/plain; charset=utf-8")
	if e != nil {
		return e
	}
	defer r.Body.Close()
	_, e = io.Copy(io.Discard, r.Body)
	return e
}
func (c *Client) Shortcut(ctx context.Context, keys ...string) error {
	return c.JSON(ctx, "POST", "/api/hid/events/send_shortcut", url.Values{"keys": {strings.Join(keys, ",")}}, nil)
}
func (c *Client) Power(ctx context.Context, action string, wait bool) error {
	switch action {
	case "on", "off", "off_hard", "reset_hard":
	default:
		return fmt.Errorf("power action must be on, off, off_hard, or reset_hard")
	}
	return c.JSON(ctx, "POST", "/api/atx/power", url.Values{"action": {action}, "wait": {fmt.Sprint(wait)}}, nil)
}
func (c *Client) Snapshot(ctx context.Context, options url.Values, dst io.Writer) error {
	r, e := c.Request(ctx, "GET", "/api/streamer/snapshot", options, nil, "")
	if e != nil {
		return e
	}
	defer r.Body.Close()
	_, e = io.Copy(dst, r.Body)
	return e
}
func (c *Client) Upload(ctx context.Context, name string, src io.Reader) error {
	r, e := c.Request(ctx, "POST", "/api/msd/write", url.Values{"image": {name}}, src, "application/octet-stream")
	if e != nil {
		return e
	}
	defer r.Body.Close()
	_, e = io.Copy(io.Discard, r.Body)
	return e
}

// resolveURL preserves base path prefixes and rejects alternate origins and inline queries.
func (c *Client) resolveURL(path string, query url.Values) (*url.URL, error) {
	p, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || p.IsAbs() || p.Host != "" || p.RawQuery != "" || p.Fragment != "" {
		return nil, fmt.Errorf("path must be an absolute path without query or fragment; pass query separately")
	}
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + p.Path
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return &u, nil
}
func (c *Client) authenticate(req *http.Request) {
	if c.token != "" {
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: c.token})
		return
	}
	if c.http.Jar != nil {
		for _, cookie := range c.http.Jar.Cookies(req.URL) {
			if cookie.Name == "auth_token" {
				return
			}
		}
	}
	if c.username != "" {
		if c.authMode == "basic" {
			req.SetBasicAuth(c.username, c.password)
		} else {
			req.Header.Set("X-KVMD-User", c.username)
			req.Header.Set("X-KVMD-Passwd", c.password)
		}
	}
}

// uploadBody ensures the Content-Length required by KVMD is present. Seekable
// readers stream directly; unknown-length readers spool to a temporary file.
func uploadBody(ctx context.Context, src io.Reader) (io.Reader, int64, func(), error) {
	done := func() {}
	if src == nil {
		return nil, 0, done, nil
	}
	if r, ok := src.(interface{ Len() int }); ok {
		return src, int64(r.Len()), done, nil
	}
	if r, ok := src.(io.Seeker); ok {
		pos, err := r.Seek(0, io.SeekCurrent)
		if err == nil {
			end, e := r.Seek(0, io.SeekEnd)
			if e == nil {
				_, e = r.Seek(pos, io.SeekStart)
				if e != nil {
					return nil, 0, done, e
				}
				return src, end - pos, done, nil
			}
		}
	}
	f, err := os.CreateTemp("", "pikvm-upload-*")
	if err != nil {
		return nil, 0, done, err
	}
	done = func() { _ = f.Close(); _ = os.Remove(f.Name()) }
	n, err := io.Copy(f, contextReader{ctx, src})
	if err != nil {
		done()
		return nil, 0, func() {}, err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		done()
		return nil, 0, func() {}, err
	}
	return f, n, done, nil
}

type contextReader struct {
	ctx context.Context
	src io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(p)
}
