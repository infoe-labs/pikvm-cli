# Repository setup

The public repository is [infoe-labs/pikvm-cli](https://github.com/infoe-labs/pikvm-cli).
The default branch is `main`. This project has no hosted production deployment,
Pages site, production environment, or deployment secrets.

The one-time empty-repository bootstrap established the source and Build workflow.
Subsequent changes use feature branches and pull requests.

## GitHub configuration

The `main` branch protection requires:

- A pull request, with zero separate approvals for this solo-maintainer repository.
- Dismissal of stale approvals when commits change.
- A successful, up-to-date `build` check from GitHub Actions (observed App ID `15368`).
- Resolution of all PR conversations.
- Linear history and enforcement for administrators.
- No force pushes, branch deletion, or bypass actors.

Only squash merges are enabled. Merged feature branches are deleted automatically.
Actions default tokens are read-only and cannot create or approve pull requests.
The workflow also explicitly grants only `contents: read`, uses read-only checkout
credentials, pins third-party Actions to verified commit SHAs, and has a finite
job timeout. Both pushes and pull requests run the `build` job without path filters.

Settings are verified by reading repository metadata, branch protection, rulesets,
Actions defaults, and environments through the GitHub API. Do not test protection
by attempting a forbidden direct push or disabling protections. A real PR and its
required check provide the delivery verification.

## Local checks and publication

Run the checks listed in [AGENTS.md](../AGENTS.md). Review staged filenames/diffs,
verify the approved author and committer, and audit all outgoing commit messages
and tracked contents before committing or pushing. Local privacy hooks are active
under `.git/hooks`; their implementation and private policy remain untracked.
Never bypass hooks. Use UTC commit timestamps.

After a merge, inspect the actual remote commit metadata and required checks. The
approved public identity must be the author; GitHub's public service committer is
allowed only for a verified server-generated merge. Select the approved noreply
address explicitly when merging. Do not add personal attribution or co-author
trailers, fabricate status checks, or self-approve PRs.

A repository copy does not transfer server settings or local hooks. Re-read these
settings when setting up a fork or a new destination, preserve stronger policies,
and report any permission or plan restriction that prevents enforcement.
