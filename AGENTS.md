# Working on pikvm-cli

Build a reusable Go library in `lib/pikvm` first. Keep Cobra and environment
configuration in `cli`; `cmd/pikvm-cli` is the executable entry point. The library
must remain importable by another Go service without depending on Cobra or the MCP SDK.
Keep MCP in `mcpserver`; reuse `Client.Invoke`, the catalog, progress parsing, and
WebSocket methods rather than duplicating protocol implementations.

## API coverage

`api/upstream.json` independently inventories the official KVMD APIs and pins all
15 WebSocket handlers by AST fingerprint. `api/ustreamer.json` records the public
uStreamer HTTP API. `api/catalog.json` adds the command mapping and body metadata.
Review upstream source when changing the inventory; do not weaken parity tests to
hide a missing route, field, service method, or CLI flag. Query, path, form, JSON,
raw-body, streaming, and WebSocket operations must retain library/CLI equivalents.

Edit the catalog and run `python3 scripts/generate-api.py`; do not hand-edit the
generated endpoint/service files or coverage table. Use
`python3 scripts/audit-api.py /path/to/kvmd /path/to/ustreamer` to audit local
upstream checkouts. Firmware-specific support is not universal; document scope and
hardware limitations accurately.

Required checks before delivery:

- `python3 scripts/generate-api.py --check`
- `python3 -m unittest discover -s scripts -p 'test_*.py'`
- `go test -race -coverpkg=./cli,./lib/pikvm,./mcpserver -covermode=atomic -coverprofile=coverage.out ./...`
- `python3 scripts/check-coverage.py coverage.out`
- `go vet ./...`
- `go build -o bin/pikvm-cli ./cmd/pikvm-cli`

## Privacy and storage

Treat source, commit history, PRs, logs, and artifacts as public. Keep personal
identities, private addresses, machine details, absolute local paths, credentials,
and user content out of them. Preserve required upstream legal notices. Use
synthetic fixtures. Do not log remote payloads, credentials, local filenames, or raw
transport exceptions. Return generic public errors while retaining inspectable
causes where useful. API output explicitly requested by the user remains output.

No telemetry, analytics, remote logging, or cloud processing. Network traffic is
limited to the user-configured PiKVM origin; credentials must not follow redirects
to another origin. Remote image fetching is an explicit user action on the device.
Session cookies remain in memory. Seekable uploads stream directly; unknown-length
uploads spool to a private temporary file and remove it afterward. Output files
are written only when requested. Do not persist credentials or snapshots by default.
Keep environment files, secrets, generated binaries, and test artifacts ignored.

## Git identity and delivery

Use the owner-approved public identity for both author and committer:
`infoe-bots <349551173+infoe-labs@users.noreply.github.com>`. Configure it locally,
never globally. Reconcile conflicting local identity settings with the owner.
Verify effective author/committer metadata before committing, use UTC timestamps,
and verify the resulting commits. Do not add personal signoffs or co-author trailers.
A verified GitHub-generated merge may use the GitHub service committer, but its
author must remain the approved identity.

Keep privacy/identity hooks and private denylists under `.git/hooks`, untracked and
executable. Check `core.hooksPath`. Never bypass or weaken hooks. Before each commit
and push, inspect staged filenames/diffs, audit all outgoing commits and their
tracked contents/messages for identity and private data, and run the intended hooks.
If published identity or secret leakage is found, stop publication and coordinate
cleanup with the owner; do not silently rewrite published history.

After the explicitly authorized initial empty-repository bootstrap, use feature
branches and pull requests. Keep `main` protected, including administrators: PRs,
strict/up-to-date passing `build` checks bound to the observed GitHub App ID,
resolved conversations, linear history, stale-review dismissal, and no force pushes,
branch deletion, or bypass actors. A solo-maintainer repository requires zero
separate approvals unless stronger policy applies. Use squash merges and automatic
feature-branch deletion. Do not fabricate statuses or self-approve PRs.

Keep Actions tokens read-only and disallow Actions from creating/approving PRs.
Run PR checks without production secrets or deployment permissions. Verify server
settings and actual remote check/commit results after publication; configuration
requests alone are not verification. This repository has no production deployment.

Core packages must meet the 90% total and handwritten statement-coverage floors.
Run default integration tests only against synthetic local servers. Real-device
tests require the integration build tag and explicit PIKVM_LIVE_TESTS=1 opt-in.
Binary releases use reviewed main-history version tags, draft asset verification,
and scoped publish permissions. Promote Homebrew formulas through protected PRs.

## Shared repository standards

Read `docs/SHARED_STANDARDS.md` for the canonical infoe-labs guidelines.
Preserve stronger project-specific privacy, review, test and release rules.
