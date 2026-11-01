# Security reporting

Use this repository's private vulnerability reporting feature to report a security
issue. Do not include device addresses, credentials, screenshots, host content, or
personal information in public issues or pull requests. Use synthetic examples.

TLS certificate verification is enabled by default. Disabling it is an explicit
operator choice. The CLI and MCP server have the permissions of the configured
PiKVM account, and mutating commands/tools can immediately affect the connected
host. MCP runs over local stdio and does not expose an HTTP listener.
