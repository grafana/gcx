---
name: release
description: Tag and release a new gcx version. Use when the user wants to cut a release, tag a version, run the release process, or says "release patch/minor/major".
---

# Releasing gcx

## Customer communications

For every major release, or minor release containing breaking changes, prepare customer communications as part of the release:

- Use [grafana-product:whats-new](https://github.com/grafana/ai-kit/tree/main/plugins/grafana-product/skills/whats-new) for new or improved capabilities.
- Use [grafana-product:whats-next](https://github.com/grafana/ai-kit/tree/main/plugins/grafana-product/skills/whats-next) for breaking changes, deprecations, removals, or disruptive behaviour changes.
- If both apply, prepare separate entries.

Before running the release command, review the changes since the previous release to identify the required entries. Inspect source PRs and compatibility changes rather than relying solely on commit prefixes or generated release notes. Reuse existing contributions where appropriate.

Read the relevant upstream skill and its required references, using `gh` to retrieve them if they are not installed. Follow their content and formatting guidance, but override their advance-notice timing: prepare the content during the release, without requiring a 2–4 week lead time or delaying the release to create one. Describe the actual release timing; do not present an already available change as upcoming.

Include affected users, exact before/after behaviour, and concrete migration instructions for breaking changes. Verify CMS metadata rather than treating a gcx version as a Grafana self-managed release version.

## Release

Automated via `mise run tag`. Requires `claude` CLI and [`svu`](https://github.com/caarlos0/svu).

```bash
mise run tag -- patch   # or minor, major
```

This generates a changelog entry (via Claude), updates `CHANGELOG.md`, `.release-notes.md`, and the Claude plugin version, commits on a `release/vX.Y.Z` branch, and pushes the branch. It does not create a tag. Then:

1. Open a PR and merge it (the script prints the exact command)
2. After merge, tag the commit on main and push the tag:
   ```bash
   git checkout main && git pull
   git tag v0.X.Y
   git push origin v0.X.Y
   ```

The tag push triggers the GoReleaser workflow.

## Contribution handoff

Before declaring the release complete, report the required contributions' draft paths and CMS submission/publication status. If submission is outstanding, name the comms owner and remaining action; a local draft alone is not a submitted contribution. If no contribution is required, state why.
