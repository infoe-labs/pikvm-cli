# Contributing

Read [AGENTS.md](AGENTS.md) and [the test guide](docs/TESTING.md). Keep device API
behavior in the reusable Go library; CLI and MCP integrations adapt its functions
and shared API catalog. Preserve endpoint/parameter parity and existing privacy
checks. Use synthetic data and never include credentials, device addresses, screen
content, or personal information in issues, fixtures, logs, or PR descriptions.

Changes use a feature branch and pull request. The required `build` check includes
coverage floors, unit tests, subprocess integration tests, and a real macOS Homebrew
installation/test. Never bypass protection or approve your own PR.
