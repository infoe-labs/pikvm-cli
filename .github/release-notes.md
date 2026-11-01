PiKVM command-line control, an importable Go API client, and an MCP stdio server.

Includes named operations for all 72 audited API routes, HID and ATX control, mass storage, GPIO, snapshots/OCR, Switch, Redfish, WebSocket events, and video APIs.

Download the archive for your OS and CPU, verify it using `SHA256SUMS`, and run `pikvm-cli --help`. Linux ARMv7 and ARM64, Linux AMD64, macOS ARM64/AMD64, and Windows ARM64/AMD64 are supported by the release builds.

Configure `PIKVM_URL`, `PIKVM_USER`, and `PIKVM_PASSWORD`, or `PIKVM_TOKEN`. Run `pikvm-cli mcp` to expose the same API environment over MCP stdio.

The release also contains a generated Homebrew formula. Promoting this formula into the repository's `Formula/` directory uses the normal protected pull-request workflow; release automation cannot write directly to main or create/approve PRs.

API support is audited against pinned upstream source revisions. Device/firmware capabilities vary. Unit and mock integration tests run in CI; physical PiKVM hardware validation remains opt-in and has not been performed for this release.
