# Releases and Homebrew

A release starts from a reviewed commit in `main` whose required `build` check is
passing. Run the checks in [TESTING.md](TESTING.md), verify the approved Git identity,
and publish a UTC annotated semantic-version tag such as `v0.1.0`. Never move an
existing release tag or bypass branch protection.

The Release workflow accepts only version tags reachable from main history. Its
validation job uses read-only permissions and reruns tests, coverage gates, and vet.
Only the publish job has `contents: write`. It produces archives for:

- Linux AMD64, ARM64, and ARMv7.
- macOS AMD64 and ARM64.
- Windows AMD64 and ARM64.

The archives, SHA256SUMS, and a generated `pikvm-cli.rb` formula are uploaded to a
draft release. The workflow downloads and checks every asset before publishing.
Prerelease semantic versions are marked as prereleases. The default Actions token
remains read-only and Actions cannot create or approve pull requests.

To build release archives locally without publishing:

```sh
python3 scripts/build-release.py --version v0.1.0 --output dist
python3 scripts/homebrew-formula.py --version v0.1.0 --checksums dist/SHA256SUMS --output dist/pikvm-cli.rb
```

## Homebrew tap

This repository is also a tap; the explicit URL avoids requiring a second
repository or a token with access to another repository:

```sh
brew tap infoe-labs/pikvm-cli https://github.com/infoe-labs/pikvm-cli
brew install infoe-labs/pikvm-cli/pikvm-cli
```

Before the first stable formula is promoted, source installation is available via
`brew install --HEAD infoe-labs/pikvm-cli/pikvm-cli`. Stable formulas use the released
macOS/Linux binaries and their recorded SHA256 checksums, without a Go build dependency.

After a release, download and verify its assets with `gh release download`, copy the
generated formula into `Formula/pikvm-cli.rb`, and submit a normal feature-branch PR.
The Homebrew CI job installs and tests those actual binaries before the required
`build` check passes. Merge the formula PR using the approved author identity.
Release automation deliberately does not push formula updates directly to main or
create PRs, preserving the repository's protection and Actions policies.
