# pikvm-cli

A reusable Go client for the PiKVM REST and WebSocket APIs, with a Cobra CLI and MCP server for people and agents.

[Binary releases](https://github.com/infoe-labs/pikvm-cli/releases) · [Tests and coverage](docs/TESTING.md) · [MCP setup](docs/MCP.md) · [Homebrew/release guide](docs/RELEASES.md)

```sh
brew tap infoe-labs/pikvm-cli https://github.com/infoe-labs/pikvm-cli
brew install infoe-labs/pikvm-cli/pikvm-cli
```

Before the first stable formula is promoted, use `brew install --HEAD infoe-labs/pikvm-cli/pikvm-cli`.

The [API coverage reference](api/COVERAGE.md) lists all 72 audited routes and their CLI equivalents. Tests check every route and parameter against an independently extracted upstream inventory, then exercise every library service method and CLI command on the wire.

The library is independent of Cobra, environment variables, and process state. Applications can import it directly; the CLI uses the same HTTP client and endpoint catalog.

```
lib/pikvm/        Importable REST client and service methods
cli/             Cobra command tree
cmd/pikvm-cli/   Installed executable entry point
main.go          Source entry point: go run main.go mcp
mcpserver/       Official MCP SDK adapter over the shared client
examples/library/ Standalone Go consumer
```

## Use from another Go service

Import the published library:

```sh
go get github.com/infoe-labs/pikvm-cli/lib/pikvm
```

```go
import (
    "context"
    "net/url"
    "time"

    "github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

client, err := pikvm.New(pikvm.Config{
    URL:      "https://pikvm.example.com",
    Username: "admin",
    Password: passwordFromYourSecretStore,
    Timeout:  30 * time.Second,
})
if err != nil {
    return err
}

ctx := context.Background() // Use your service's request context.
state, err := client.ATX().State(ctx, nil)
if err != nil {
    return err
}
_ = state // JSON response envelope; decode into your own application types.

_, err = client.MSD().Params(ctx, url.Values{
    "image": {"installer.iso"},
    "cdrom": {"1"},
})
return err
```

Services are available through `System()`, `Auth()`, `HID()`, `ATX()`, `MSD()`, `GPIO()`, `Streamer()`, `Switch()`, `Redfish()`, `Alerts()`, `Events()`, `Media()`, and `Video()`. Their exported methods cover the documented HTTP operations. Query options use the original API names, such as `to_x`, and are URL encoded by the client. JSON methods return `pikvm.Result` (`json.RawMessage`), retaining the full `ok`/`result` envelope and firmware-specific fields. Empty successful responses return an empty result.

Convenience methods include `Info`, `TypeText`, `Shortcut`, `Power`, `Snapshot`, and `Upload`:

```go
err = client.Shortcut(ctx, "ControlLeft", "KeyL")
err = client.TypeText(ctx, "https://example.com", nil)
err = client.Snapshot(ctx, nil, destinationWriter)
err = client.Upload(ctx, "installer.iso", sourceReader)
```

Each operation takes a context. Transfer methods accept readers/writers, so images do not have to fit in memory. Service methods returning `*http.Response` leave body consumption and closing to the caller. `Request` provides arbitrary HTTP requests; `JSON` decodes a response into an application-provided value. `APIError` exposes HTTP status, method, path, and a generic status message (server error payloads are not included), and can be inspected with `errors.As`.

`Config.HTTPClient` supports custom transports and cookie jars. If no jar is provided, the client creates one. `Config.AuthMode` selects `headers` (default) or `basic`; use the CLI flag `--auth-mode basic` for HTTP Basic authentication. The default client verifies TLS, blocks redirects outside the configured origin, and uses a 30-second timeout. A negative library timeout disables the timeout. `Insecure: true` explicitly permits self-signed certificates. To authenticate with a session, create a client and call `Login(ctx, user, password)`; the default cookie jar retains the session. `Config.Token` accepts an existing session token. With 2FA, append the current OTP to the password as specified by PiKVM.

For a local consumer before publication, add a replace directive pointing at this checkout:

```sh
go mod edit -replace github.com/infoe-labs/pikvm-cli=/absolute/path/to/pikvm-cli
go get github.com/infoe-labs/pikvm-cli/lib/pikvm
```

## Build and run

Go 1.25 or newer (required by the official MCP SDK):

```sh
go build -o bin/pikvm-cli ./cmd/pikvm-cli
./bin/pikvm-cli --help
```

Install from source with:

```sh
go install github.com/infoe-labs/pikvm-cli/cmd/pikvm-cli@latest
```

Configure the connection using environment variables or the corresponding global flags:

| Environment | Flag | Meaning |
| --- | --- | --- |
| `PIKVM_URL` | `--url` | Base URL, including `https://` |
| `PIKVM_USER` | `--user` | PiKVM username |
| `PIKVM_PASSWORD` | `--password` | Password, including OTP when required |
| `PIKVM_TOKEN` | `--token` | Session cookie token, preferred over user/password |

Prefer environment variables or your secret manager over credentials in shell arguments. Use `--insecure` explicitly for devices with self-signed certificates. `--request-timeout 0s` disables the HTTP timeout for long transfers; Ctrl-C still cancels the request.

```sh
export PIKVM_URL=https://pikvm.example.com
export PIKVM_USER=admin
# Supply PIKVM_PASSWORD through your secret manager or shell environment.

pikvm-cli system info --fields hw
pikvm-cli hid shortcut --keys ControlLeft,KeyL
pikvm-cli hid type --body 'https://example.com'
printf 'Text from stdin' | pikvm-cli hid type --input -
pikvm-cli hid key --key Enter
pikvm-cli hid move --to-x 0 --to-y 0
pikvm-cli hid button --button left
pikvm-cli atx state
pikvm-cli atx power --action on --wait true
pikvm-cli streamer snapshot --output screen.jpg
pikvm-cli streamer snapshot --ocr true --ocr-langs eng
pikvm-cli --request-timeout 0s msd upload --image installer.iso --input installer.iso
pikvm-cli msd params --image installer.iso --cdrom true
pikvm-cli msd connect --connected true
pikvm-cli switch active --port 2
pikvm-cli redfish power --id 0 --body '{"ResetType":"On"}'
```

`auth login` uses the configured username/password and emits `{"auth_token":"..."}` for reuse through `PIKVM_TOKEN`. Login output contains a credential. Use `--output` if saving it. `auth check` checks authentication and `auth logout` invalidates the supplied session.

Commands are grouped under `system`, `auth`, `hid`, `atx`, `msd`, `gpio`, `streamer`, `switch`, `redfish`, `alerts`, `events`, `media`, and `video`. Use `<group> <command> --help` for parameter flags. Query flags accept explicit values, including booleans (`--connected true`, `--state false`). Flags use hyphens where the REST parameter uses underscores. Power, GPIO, keyboard, and storage operations take effect immediately.

Successful JSON responses go to stdout, retaining the PiKVM response envelope; errors go to stderr with a nonzero exit code. JPEGs, OCR, logs, metrics, and download progress retain their original response formats. `--output FILE` writes raw response bytes. Request bodies come from `--body TEXT` or `--input FILE`; `--input -` reads stdin. Redfish bodies use JSON, HID text uses plain text, MSD uploads use binary, and Switch colors use query flags (for example `switch colors --beacon 'FFA500:BF:0028'`). The Switch color handler in current KVMD reads query parameters, despite the handbook showing a plain body.

Use `request` for arbitrary endpoints:

```sh
pikvm-cli request GET /api/info -q fields=hw
pikvm-cli request POST /api/hid/events/send_key -q key=Enter
```

Pass query parameters separately using repeatable `-q key=value`. Cobra also supplies shell completion through `pikvm-cli completion`.

## Scope and verification

Coverage is pinned to the official KVMD and uStreamer source revisions recorded in [api/COVERAGE.md](api/COVERAGE.md), including operations absent from the handbook. All known query parameters have named flags; login form and Redfish JSON fields also have named flags. For example:

```sh
pikvm-cli auth login --expire 3600 --redirect /kvm/
pikvm-cli msd fetch --source-url https://example.com/installer.iso --verify true --activate true
pikvm-cli msd download --image installer.iso --compress zstd --output installer.iso.zst
pikvm-cli streamer params --quality 90 --desired-fps 30 --h264-bitrate 5000 --h264-gop 30
pikvm-cli switch colors --active '00FF00:FF:0000' --beacon 'FFA500:BF:0028'
pikvm-cli redfish power --id 0 --reset-type On
pikvm-cli redfish insert --image installer.iso --inserted true --write-protected true --drive-optical true
pikvm-cli alerts state
```

API parameters that would shadow device connection flags are renamed: `url` becomes `--source-url`, MSD `user` becomes `--remote-user`, and the legacy remote download `insecure` parameter becomes `--remote-insecure`. Login supports `--login-user` and `--login-password` to override the configured credentials in its form. Login preserves redirect responses so it can return the session token without following an HTML redirect. Raw `--body`/`--input` is mutually exclusive with individual body-field flags.

Uploads send the Content-Length required by KVMD. Seekable files stream directly; unknown-length readers, including stdin, spool to a temporary file before transmission and are removed afterward. Remote downloads relay every NDJSON progress record and return a nonzero exit status if a later record reports failure.

### WebSocket events and video

```sh
pikvm-cli events connect --stream false
pikvm-cli events key --key KeyA --state=true --finish
pikvm-cli events button --button left --state=false
pikvm-cli events move --to-x 0 --to-y 0
pikvm-cli events relative --delta '[{"x":10,"y":5},{"x":-2,"y":0}]' --squash
pikvm-cli events wheel --delta '{"x":0,"y":1}'
pikvm-cli events connect --stream false --event-type ping --event '{}' --count 1
pikvm-cli events binary --binary-input frame.bin
pikvm-cli media state
pikvm-cli media start --type video --format h264 --count 10
pikvm-cli media connect --video jpeg --count 10
pikvm-cli media keyframe
pikvm-cli --request-timeout 0s video mjpeg --output stream.mjpeg
```

`events connect` and `media connect` receive until canceled unless `--count` is set. Named send commands exit after writing by default; add `--count` to receive messages afterward. Received text events are JSON lines; binary messages are JSON records with `message_type` and `data_base64` to preserve frame boundaries. Initial subsystem messages may arrive before a response to a sent event. A successful send does not acknowledge completion of the hardware action. WebSocket boolean flags such as `--state=false` use Cobra's normal boolean syntax.

Use `--body` or `--input` for a complete JSON event envelope, `--event-type` plus `--event` for arbitrary event fields, or `--binary-input` for complete binary frames including the opcode. This covers all 15 inbound JSON/binary handlers in the audited upstream. Binary KVMD opcodes are 0 (ping), 1 (key), 2 (button), 3 (absolute move), 4 (relative move), and 5 (wheel); media opcodes are 0 (ping) and 1 (request keyframe).

Go services can use `client.Events().Connect(ctx, options)` or `client.Media().Connect(ctx, options)`, then `Session.Read`, `SendEvent`, `SendJSON`, `SendBinary`, or the named `Key`, `MouseButton`, `MouseMove`, `MouseRelative`, `MouseWheel`, `Ping`, `StartMedia`, and `RequestKeyframe` methods. Close sessions after use. `Auth().LoginWithOptions`, `Redfish().ResetSystem`, `Redfish().InsertMedia`, and `MSD().UploadWithOptions` also provide typed body/upload options.

The inventory covers main KVMD, KVMD-Media, and uStreamer HTTP APIs exposed through standard PiKVM Nginx. The separate Janus WebRTC signaling API, optional third-party extras, and internal Unix-socket daemons are outside this inventory. Snapshot availability depends on the device's streamer configuration; an unavailable stream can return HTTP 503. Device firmware and hardware determine which endpoints and parameters are supported. Endpoint/parameter coverage does not imply 100% code coverage or physical hardware verification.

```sh
go test -race -coverpkg=./cli,./lib/pikvm,./mcpserver -covermode=atomic -coverprofile=coverage.out ./...
python3 scripts/check-coverage.py coverage.out
go vet ./...
python3 scripts/generate-api.py --check
# Optional: compare local checkouts of the official upstream projects.
python3 scripts/audit-api.py /path/to/kvmd /path/to/ustreamer
```

Tests exercise HTTP authentication, sessions, request encoding, binary transfers, CLI dispatch and validation, TLS defaults, redirect boundaries, error handling, and cancellation against local HTTP servers. No physical PiKVM is required for these tests; hardware integration has not been verified.

References: [PiKVM HTTP API](https://docs.pikvm.org/api/), [guanana/pikvm-lib](https://github.com/guanana/pikvm-lib), and [PiKVM Remote Control skill](https://mcpmarket.com/tools/skills/pikvm-remote-control).

Repository protection, CI, and publication conventions are documented in [docs/REPOSITORY_SETUP.md](docs/REPOSITORY_SETUP.md).

Run `go run main.go mcp` or `pikvm-cli mcp` with the same PiKVM environment to expose the API over MCP stdio. The adapter reuses the library and API catalog; see [docs/MCP.md](docs/MCP.md) for tools, limits, and client configuration.
