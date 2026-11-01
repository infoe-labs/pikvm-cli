package pikvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// OperationInput is shared by CLI, MCP, and embedding services. Parameters use
// API wire names. Fields use form names or dotted JSON paths from the catalog.
// Body overrides field encoding and is mutually exclusive with Fields.
type OperationInput struct {
	Parameters url.Values
	Fields     map[string]any
	Body       io.Reader
}

// Invoke executes an HTTP operation from the catalog using the same transport as
// the named services. It resolves path parameters and encodes form/JSON bodies.
// The caller must close the response body. Use Dial for WebSocket operations.
func (c *Client) Invoke(ctx context.Context, ep Endpoint, input OperationInput) (*http.Response, error) {
	if ep.WebSocket {
		return nil, fmt.Errorf("use Dial for WebSocket operations")
	}
	q := url.Values{}
	for key, values := range input.Parameters {
		q[key] = append([]string(nil), values...)
	}
	path := ep.Path
	if strings.Contains(path, "{id}") {
		id := q.Get("id")
		if id == "" {
			id = "0"
		}
		if strings.ContainsAny(id, "/?#") {
			return nil, fmt.Errorf("invalid system id")
		}
		path = strings.ReplaceAll(path, "{id}", url.PathEscape(id))
		q.Del("id")
	}
	for _, name := range ep.Parameters {
		if strings.HasSuffix(name, "*") {
			if _, ok := q[strings.TrimSuffix(name, "*")]; !ok {
				return nil, fmt.Errorf("required API parameter is missing")
			}
		}
	}
	if input.Body != nil && len(input.Fields) > 0 {
		return nil, fmt.Errorf("body and individual fields are mutually exclusive")
	}
	body := input.Body
	ct := ep.ContentType
	if body == nil && len(ep.BodyParameters) > 0 {
		fields := map[string]any{}
		form := url.Values{}
		if ct == "application/x-www-form-urlencoded" {
			form.Set("user", c.username)
			form.Set("passwd", c.password)
		}
		for _, parameter := range ep.BodyParameters {
			value, ok := input.Fields[parameter.Name]
			if parameter.Required && !ok {
				return nil, fmt.Errorf("required body field is missing")
			}
			if !ok {
				continue
			}
			if ct == "application/x-www-form-urlencoded" {
				text, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("form fields must be strings")
				}
				form.Set(parameter.Name, text)
				continue
			}
			if parameter.Type == "bool" {
				if _, ok := value.(bool); !ok {
					return nil, fmt.Errorf("body field must be boolean")
				}
			}
			parts := strings.Split(parameter.Name, ".")
			dst := fields
			for _, part := range parts[:len(parts)-1] {
				nested, ok := dst[part].(map[string]any)
				if !ok {
					nested = map[string]any{}
					dst[part] = nested
				}
				dst = nested
			}
			dst[parts[len(parts)-1]] = value
		}
		if ct == "application/x-www-form-urlencoded" {
			body = strings.NewReader(form.Encode())
		} else {
			data, err := json.Marshal(fields)
			if err != nil {
				return nil, fmt.Errorf("body fields cannot be encoded")
			}
			body = strings.NewReader(string(data))
		}
	} else if body == nil && ep.BodyRequired {
		return nil, fmt.Errorf("request body is required")
	}
	if ct == "" && body != nil {
		ct = "application/json"
	}
	return c.Request(ctx, ep.Method, path, q, body, ct)
}
