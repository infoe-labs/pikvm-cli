package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
	"github.com/spf13/cobra"
)

func addSocketFlags(cmd *cobra.Command) {
	cmd.Flags().String("stream", "", "KVMD handshake stream parameter (true/false)")
	cmd.Flags().String("video", "", "Media handshake video format (h264/jpeg)")
	cmd.Flags().String("event-type", "", "Send a JSON event of this type")
	cmd.Flags().String("event", "{}", "Event object as JSON; used with --event-type")
	cmd.Flags().String("body", "", "Send a complete JSON event envelope")
	cmd.Flags().String("input", "", "Read a JSON event envelope from file or - for stdin")
	cmd.Flags().String("binary-input", "", "Send a complete binary frame from file or - for stdin")
	cmd.Flags().Int("count", 0, "Number of received messages; 0 watches until canceled (send commands exit after sending)")
	cmd.Flags().StringP("output", "o", "", "Write received frames as JSON lines to file or stdout")
	cmd.MarkFlagsMutuallyExclusive("event-type", "body", "input", "binary-input")
}
func runSocket(cmd *cobra.Command, cfg pikvm.Config, path string, send func(context.Context, *pikvm.Session) error, sendOnly bool) error {
	count, _ := cmd.Flags().GetInt("count")
	if count < 0 {
		return fmt.Errorf("count cannot be negative")
	}
	q := url.Values{}
	for _, name := range []string{"stream", "video"} {
		if cmd.Flags().Changed(name) {
			v, _ := cmd.Flags().GetString(name)
			if name == "stream" && path != "/api/ws" || name == "video" && path != "/api/media/ws" {
				return fmt.Errorf("--%s is not supported by this WebSocket", name)
			}
			q.Set(name, v)
		}
	}
	// Validate/read payloads before opening the connection.
	var payload []byte
	kind := ""
	for _, flag := range []string{"input", "binary-input", "body", "event-type"} {
		if !cmd.Flags().Changed(flag) {
			continue
		}
		kind = flag
		v, _ := cmd.Flags().GetString(flag)
		if flag == "input" || flag == "binary-input" {
			src, done, e := openInput(cmd, v, "")
			if e != nil {
				return e
			}
			payload, e = io.ReadAll(src)
			done()
			if e != nil {
				return e
			}
		} else if flag == "event-type" {
			event, _ := cmd.Flags().GetString("event")
			if !json.Valid([]byte(event)) {
				return fmt.Errorf("invalid event JSON")
			}
			payload, _ = json.Marshal(map[string]any{"event_type": v, "event": json.RawMessage(event)})
		} else {
			payload = []byte(v)
		}
	}
	if kind != "" && kind != "binary-input" && !json.Valid(payload) {
		return fmt.Errorf("invalid JSON event")
	}
	client, e := newClient(cfg)
	if e != nil {
		return e
	}
	socket, e := client.Dial(cmd.Context(), path, q)
	if e != nil {
		return e
	}
	defer socket.Close()
	if send != nil {
		if kind != "" {
			return fmt.Errorf("named event commands cannot be combined with another event payload")
		}
		if e = send(cmd.Context(), socket); e != nil {
			return e
		}
	}
	if kind == "binary-input" {
		e = socket.SendBinary(cmd.Context(), payload)
	} else if kind != "" {
		e = socket.SendJSON(cmd.Context(), payload)
	}
	if e != nil {
		return e
	}
	if sendOnly && count == 0 {
		return nil
	}
	out := cmd.OutOrStdout()
	output, _ := cmd.Flags().GetString("output")
	if output != "" && output != "-" {
		f, e := os.Create(output)
		if e != nil {
			return e
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)
	for n := 0; count == 0 || n < count; n++ {
		msg, e := socket.Read(cmd.Context())
		if e != nil {
			return e
		}
		var value any
		if msg.Type == "text" && json.Valid(msg.Data) {
			value = json.RawMessage(msg.Data)
		} else {
			value = map[string]string{"message_type": msg.Type, "data_base64": base64.StdEncoding.EncodeToString(msg.Data)}
		}
		if e = enc.Encode(value); e != nil {
			return e
		}
	}
	return nil
}
func addSocketCommands(events, media *cobra.Command, cfg *pikvm.Config) {
	for _, spec := range []struct {
		group             *cobra.Command
		name, path, event string
	}{
		{events, "ping", "/api/ws", "ping"}, {events, "key", "/api/ws", "key"}, {events, "button", "/api/ws", "mouse_button"}, {events, "move", "/api/ws", "mouse_move"}, {events, "relative", "/api/ws", "mouse_relative"}, {events, "wheel", "/api/ws", "mouse_wheel"}, {events, "binary", "/api/ws", ""},
		{media, "start", "/api/media/ws", "start"}, {media, "ping", "/api/media/ws", ""}, {media, "keyframe", "/api/media/ws", ""},
	} {
		spec := spec
		cmd := &cobra.Command{Use: spec.name, Short: "Send WebSocket " + spec.name, Args: cobra.NoArgs}
		addSocketFlags(cmd)
		switch spec.event {
		case "key":
			cmd.Flags().String("key", "", "Keyboard key name")
			cmd.Flags().Bool("state", false, "Pressed state")
			cmd.Flags().Bool("finish", false, "Release non-modifier keys after pressing")
			_ = cmd.MarkFlagRequired("key")
			_ = cmd.MarkFlagRequired("state")
		case "mouse_button":
			cmd.Flags().String("button", "", "Mouse button name")
			cmd.Flags().Bool("state", false, "Pressed state")
			_ = cmd.MarkFlagRequired("button")
			_ = cmd.MarkFlagRequired("state")
		case "mouse_move":
			cmd.Flags().Int("to-x", 0, "Absolute X coordinate")
			cmd.Flags().Int("to-y", 0, "Absolute Y coordinate")
			_ = cmd.MarkFlagRequired("to-x")
			_ = cmd.MarkFlagRequired("to-y")
		case "mouse_relative", "mouse_wheel":
			cmd.Flags().String("delta", "", "JSON delta object or list of objects, each with x/y")
			cmd.Flags().Bool("squash", false, "Combine delta events")
			_ = cmd.MarkFlagRequired("delta")
		case "start":
			cmd.Flags().String("type", "video", "Media type")
			cmd.Flags().String("format", "", "Media format (h264/jpeg)")
			_ = cmd.MarkFlagRequired("format")
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if spec.name == "binary" && !cmd.Flags().Changed("binary-input") {
				return fmt.Errorf("--binary-input is required")
			}
			var send func(context.Context, *pikvm.Session) error
			if spec.name != "binary" {
				send = func(ctx context.Context, s *pikvm.Session) error {
					event := map[string]any{}
					switch spec.event {
					case "key":
						key, _ := cmd.Flags().GetString("key")
						state, _ := cmd.Flags().GetBool("state")
						finish, _ := cmd.Flags().GetBool("finish")
						event = map[string]any{"key": key, "state": state, "finish": finish}
					case "mouse_button":
						button, _ := cmd.Flags().GetString("button")
						state, _ := cmd.Flags().GetBool("state")
						event = map[string]any{"button": button, "state": state}
					case "mouse_move":
						x, _ := cmd.Flags().GetInt("to-x")
						y, _ := cmd.Flags().GetInt("to-y")
						event["to"] = map[string]int{"x": x, "y": y}
					case "mouse_relative", "mouse_wheel":
						delta, _ := cmd.Flags().GetString("delta")
						if !json.Valid([]byte(delta)) {
							return fmt.Errorf("invalid delta JSON")
						}
						squash, _ := cmd.Flags().GetBool("squash")
						event = map[string]any{"delta": json.RawMessage(delta), "squash": squash}
					case "start":
						typ, _ := cmd.Flags().GetString("type")
						format, _ := cmd.Flags().GetString("format")
						event = map[string]any{"type": typ, "format": format}
					case "":
						opcode := byte(0)
						if spec.name == "keyframe" {
							opcode = 1
						}
						return s.SendBinary(ctx, []byte{opcode})
					}
					return s.SendEvent(ctx, spec.event, event)
				}
			}
			return runSocket(cmd, *cfg, spec.path, send, true)
		}
		spec.group.AddCommand(cmd)
	}
}
