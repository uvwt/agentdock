# Code signing policy

Free code signing provided by [SignPath.io](https://signpath.io), certificate by [SignPath Foundation](https://signpath.org).

## What is signed

Official AgentDock Windows release executables are built from this repository by GitHub Actions. AgentDock-owned Windows executables are Authenticode-signed through SignPath.io when the SignPath Foundation production certificate is active. Third-party binaries, such as `cloudflared`, keep their upstream signatures and are not re-signed with the AgentDock certificate.

The signing build definition is [`.github/workflows/release.yml`](../.github/workflows/release.yml). Production signing is restricted to `v*` release refs from this repository, uses SignPath trusted-build-system and origin verification, and requires a manual approval before the release certificate can be used.

## Roles

AgentDock is currently maintained by a single trusted maintainer:

- **Authors / committers:** [@uvwt](https://github.com/uvwt). The maintainer is trusted to modify the project source and build configuration without an additional review.
- **Reviewers:** [@uvwt](https://github.com/uvwt). Contributions from other authors are reviewed before merge.
- **Approvers:** [@uvwt](https://github.com/uvwt). Each production SignPath signing request requires manual approval.

## Privacy and network access

AgentDock does not include usage analytics or telemetry.

AgentDock can communicate with networked systems when the user or operator explicitly configures or invokes features that require network access. Examples include configured remote MCP or plugin endpoints, Cloudflare Tunnel, and user-initiated update checks or downloads. Third-party services used by those features are governed by their respective privacy policies.

AgentDock does not silently upload local files or conversation data to a service operated by the AgentDock project.
