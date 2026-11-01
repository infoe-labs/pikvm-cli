# Testing and coverage

The default suite is hermetic: it uses synthetic data, local HTTP/TLS/WebSocket
servers, the official MCP SDK client, and compiled subprocesses. It does not use
real PiKVM credentials or contact a configured device. CLI unit tests clear inherited
connection settings; subprocess tests supply their own isolated environment.

```sh
python3 scripts/generate-api.py --check
python3 -m unittest discover -s scripts -p 'test_*.py'
go test -race -coverpkg=./cli,./lib/pikvm,./mcpserver -covermode=atomic -coverprofile=coverage.out ./...
python3 scripts/check-coverage.py coverage.out
go tool cover -html=coverage.out -o bin/coverage.html
go vet ./...
```

CI enforces at least **90% statement coverage for each** of `cli`, `lib/pikvm`,
and `mcpserver`. It separately applies the same floor to handwritten code, excluding
only the generated `lib/pikvm/services.go` and `lib/pikvm/endpoints.go` files.
Duplicate blocks in cross-package coverage profiles are merged rather than counted
twice. Missing/malformed profiles fail closed. Coverage does not include subprocess
execution; the executable entry points are tested separately as integration tests.
Examples and test-only helpers are outside this core-package coverage gate.

Tests cover every audited route and parameter, success and HTTP/transport errors,
redirect boundaries, cookie/header/Basic authentication, TLS, cancellation, typed
Redfish bodies, binary upload length/spooling/cleanup, file output, JSON precision,
NDJSON progress and late errors, WebSocket text/binary events and limits, MCP tool
schemas and all catalog tools, image content, and public error privacy. Compiled
process tests check exit status, stdout/stderr separation, environment credentials,
TLS rejection, uploads/snapshots, version stamping, WebSocket exchange, MCP stdio,
and the exact `go run main.go mcp` entry point.

The CI matrix tests the minimum Go 1.25 series and current stable Go. A macOS job
installs this checkout as a Homebrew tap, installs the formula, and runs its test.
The stable required `build` check passes only when all Go and Homebrew jobs pass.

## Optional real-device integration

This requires explicit opt-in and is excluded from ordinary tests:

```sh
# Supply PIKVM_URL and either PIKVM_USER/PIKVM_PASSWORD or PIKVM_TOKEN securely.
PIKVM_LIVE_TESTS=1 go test -tags integration ./integration -run '^TestLivePiKVMReadOnly$' -v
```

For a self-signed device certificate, explicitly set `PIKVM_INSECURE=1`. The suite
reads subsystem state and sends a WebSocket ping with `stream=false`. It never sends
HID input, changes power/storage/GPIO, starts video, or logs in/out. Unsupported
404/501 state endpoints are skipped; authentication and other failures fail the
suite. Errors deliberately omit device addresses, credentials, and response data.
CI compiles this suite and verifies its opt-out path without supplying device secrets.
No physical PiKVM test is claimed unless the suite was actually run against one.

## Release-tooling checks

Python unit tests validate semantic versions, deterministic tar/zip packaging,
executable archive modes, formula platform/checksum selection, and coverage parsing.
A release workflow rebuilds and tests the tagged main-history commit, cross-compiles
seven targets, publishes a draft, downloads every asset, verifies SHA256SUMS, and
only then makes the release public. Reproducible bytes require the same Go toolchain;
archives contain no local paths or build timestamps.
