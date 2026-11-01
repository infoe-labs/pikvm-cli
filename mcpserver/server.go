// Package mcpserver adapts the shared PiKVM API catalog to the official MCP SDK.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxResponseBytes = 8 << 20

// ToolName is stable across CLI builds and derived directly from the API catalog.
func ToolName(ep pikvm.Endpoint) string {
	return "pikvm_" + ep.Group + "_" + strings.ReplaceAll(ep.Name, "-", "_")
}

// New creates an MCP server around an existing reusable client. It starts no
// network listener and logs no credentials or API payloads. All tools act on the
// client-configured device; callers cannot override the transport origin.
func New(client *pikvm.Client, version string) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "pikvm-cli", Version: version}, &sdk.ServerOptions{Capabilities: &sdk.ServerCapabilities{}, Instructions: "Control the configured PiKVM. Mutating tools affect the connected host immediately. WebSocket calls are bounded exchanges. API responses may contain host screen content."})
	for _, ep := range pikvm.Endpoints() {
		ep := ep
		readOnly := ep.Method == "GET" && !ep.WebSocket && ep.Path != "/api/streamer/snapshot"
		tool := &sdk.Tool{Name: ToolName(ep), Description: ep.Description + " (" + ep.Method + " " + ep.Path + ")", InputSchema: schema(ep), Annotations: &sdk.ToolAnnotations{ReadOnlyHint: readOnly, IdempotentHint: readOnly}}
		sdk.AddTool(server, tool, func(ctx context.Context, _ *sdk.CallToolRequest, args map[string]any) (*sdk.CallToolResult, any, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			result, err := invoke(ctx, client, ep, args)
			if err != nil {
				return nil, nil, fmt.Errorf("PiKVM operation failed; check parameters, authentication, device support, and connection")
			}
			return result, result.StructuredContent, nil
		})
	}
	return server
}
func schema(ep pikvm.Endpoint) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, name := range ep.Parameters {
		key := strings.TrimSuffix(name, "*")
		properties[key] = map[string]any{"type": "string", "description": "API parameter " + key}
		if strings.HasSuffix(name, "*") {
			required = append(required, key)
		}
	}
	for _, field := range ep.BodyParameters {
		typ := field.Type
		if typ == "bool" {
			typ = "boolean"
		}
		properties[field.Name] = map[string]any{"type": typ, "description": "Request body field " + field.Name}
	}
	if ep.ContentType != "" {
		properties["body"] = map[string]any{"type": "string", "description": "Raw request body; mutually exclusive with body fields"}
		properties["body_base64"] = map[string]any{"type": "string", "description": "Base64-encoded body for binary uploads"}
	}
	if ep.WebSocket {
		properties["event_type"] = map[string]any{"type": "string"}
		properties["event"] = map[string]any{"type": "object", "additionalProperties": true}
		properties["binary_base64"] = map[string]any{"type": "string", "description": "Complete binary frame, including opcode"}
		properties["count"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 32, "description": "Received messages (default 1)"}
	}
	result := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}
func invoke(ctx context.Context, client *pikvm.Client, ep pikvm.Endpoint, args map[string]any) (*sdk.CallToolResult, error) {
	input := pikvm.OperationInput{Parameters: url.Values{}, Fields: map[string]any{}}
	for _, parameter := range ep.Parameters {
		key := strings.TrimSuffix(parameter, "*")
		if value, ok := args[key]; ok {
			input.Parameters.Set(key, value.(string))
		}
	}
	for _, field := range ep.BodyParameters {
		if value, ok := args[field.Name]; ok {
			input.Fields[field.Name] = value
		}
	}
	if ep.WebSocket {
		return exchange(ctx, client, ep, input.Parameters, args)
	}
	raw, hasRaw := args["body"]
	encoded, hasEncoded := args["body_base64"]
	if hasRaw && hasEncoded {
		return nil, fmt.Errorf("body formats are mutually exclusive")
	}
	if hasRaw {
		input.Body = strings.NewReader(raw.(string))
	}
	if hasEncoded {
		data, err := base64.StdEncoding.DecodeString(encoded.(string))
		if err != nil {
			return nil, err
		}
		input.Body = bytes.NewReader(data)
	}
	response, err := client.Invoke(ctx, ep, input)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("response exceeds MCP limit")
	}
	contentType := response.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "image/") {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.ImageContent{Data: data, MIMEType: strings.Split(contentType, ";")[0]}}}, nil
	}
	if len(data) == 0 {
		data = []byte(`{"ok":true}`)
	}
	if strings.Contains(contentType, "ndjson") {
		if err := pikvm.CopyProgress(io.Discard, bytes.NewReader(data)); err != nil {
			return nil, err
		}
	}
	if json.Valid(data) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}, StructuredContent: json.RawMessage(data)}, nil
	}
	if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "ndjson") {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}, nil
	}
	value := map[string]string{"content_type": contentType, "data_base64": base64.StdEncoding.EncodeToString(data)}
	data, _ = json.Marshal(value)
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}, StructuredContent: value}, nil
}
func exchange(ctx context.Context, client *pikvm.Client, ep pikvm.Endpoint, query url.Values, args map[string]any) (*sdk.CallToolResult, error) {
	session, err := client.Dial(ctx, ep.Path, query)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	session.SetReadLimit(maxResponseBytes)
	if typ, ok := args["event_type"]; ok {
		if _, binary := args["binary_base64"]; binary {
			return nil, fmt.Errorf("event formats are mutually exclusive")
		}
		if err = session.SendEvent(ctx, typ.(string), args["event"]); err != nil {
			return nil, err
		}
	}
	if encoded, ok := args["binary_base64"]; ok {
		data, e := base64.StdEncoding.DecodeString(encoded.(string))
		if e != nil {
			return nil, e
		}
		if err = session.SendBinary(ctx, data); err != nil {
			return nil, err
		}
	}
	count := 1
	if value, ok := args["count"]; ok {
		count = int(value.(float64))
	}
	messages := make([]any, 0, count)
	totalBytes := 0
	for range count {
		message, e := session.Read(ctx)
		if e != nil {
			return nil, e
		}
		totalBytes += len(message.Data)
		if totalBytes > maxResponseBytes {
			return nil, fmt.Errorf("WebSocket exchange exceeds MCP limit")
		}
		if message.Type == "text" && json.Valid(message.Data) {
			messages = append(messages, json.RawMessage(message.Data))
		} else {
			messages = append(messages, map[string]string{"message_type": message.Type, "data_base64": base64.StdEncoding.EncodeToString(message.Data)})
		}
	}
	value := map[string]any{"messages": messages}
	data, _ := json.Marshal(value)
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}, StructuredContent: value}, nil
}
