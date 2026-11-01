# MCP server

The server uses the [official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk).
It is a stdio adapter over the same `lib/pikvm` client, API catalog, shared `Invoke`
request encoder, streaming-progress parser, and WebSocket session methods as the
CLI. It does not implement a second PiKVM transport or start an HTTP listener.

```sh
# Supply PiKVM connection credentials through your environment/secret manager.
go run main.go mcp
# Or use the installed/released binary:
pikvm-cli mcp
```

Both entry points read `PIKVM_URL`, `PIKVM_USER`, `PIKVM_PASSWORD`, and `PIKVM_TOKEN`.
Global CLI flags, including `--insecure` and `--auth-mode basic`, also apply. Stdout
contains only MCP protocol messages. Errors omit connection details and API payloads.

Example client configuration using an already configured environment:

```json
{
  "mcpServers": {
    "pikvm": {
      "command": "pikvm-cli",
      "args": ["mcp"]
    }
  }
}
```

Your MCP host must pass the configured environment to the process. Do not put
credentials in a checked-in MCP client configuration.

Tools have stable names such as `pikvm_system_info`, `pikvm_hid_shortcut`,
`pikvm_streamer_snapshot`, and `pikvm_redfish_insert`. There is one tool for each
of the 72 audited API routes. Query parameters use original wire names (`to_x`,
`url`, etc.); form/JSON properties use their catalog field names. Nested body
properties use dotted paths, such as `Oem.PiKVM.DriveOptical`. The shared encoder
turns these into the correct nested JSON. Use `body` for a raw body or `body_base64`
for an upload; these are mutually exclusive with each other and body-field arguments.
Schemas reject unknown properties and invalid types before contacting the device.

JSON results provide structured content plus text, snapshots return MCP image
content, and binary downloads return base64 data. HTTP and WebSocket results are
bounded to 8 MiB and tool calls have a 30-second limit. Large disk images and
continuous video transfers belong in the streaming CLI/library interfaces.

The two WebSocket tools accept handshake parameters, an optional `event_type` and
`event` object or `binary_base64` frame, and a receive `count` from 1 to 32 (default
1). Initial subsystem messages may arrive before an event's response. Socket reads
and writes reuse the library, and the connection closes when the bounded exchange
finishes. This is not a persistent interactive session.

Mutating tools immediately affect the connected host with the permissions of the
configured PiKVM account. Tool annotations identify read-only operations, but they
are hints rather than an authorization boundary. Use an MCP client and device
account appropriate to the access you intend to grant.
