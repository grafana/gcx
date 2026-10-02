---
name: prepare-release
description: Use when preparing a new gcx release PR, choosing a patch/minor/major version, or refreshing an existing release PR's changelog and communications before review.
---

# Prepare a gcx release

Produce a reviewable release PR, then stop. This stage does not merge PRs, create release tags, publish customer communications, or announce the release. Approval alone does not start publication; a maintainer invokes `publish-release` separately.

## Prepare or refresh

1. Read repository contribution and validation instructions. Use a clean isolated checkout; preserve unrelated work. Fetch the upstream base and tags, identify an existing release PR before creating another, and record the previous release tag and exact source base SHA. Inspect the source PRs in that range for user-facing changes, compatibility, deprecations, and removals. Resolve the requested version/bump against those facts and the repository's compatibility rules.
2. Read [changelog review](references/changelog-review.md) before generating or refreshing artifacts. `mise run tag -- patch` (or `minor`, `major`) normally creates, commits, and pushes a release branch; it does **not** create the final release tag. Use its generation-only path, `DRY_RUN=1 mise run tag -- patch`, from the frozen source base so review and repository gates precede committing. Requires `claude` and `svu`. Confirm the computed version equals the intended version; stop on mismatch.
3. Review generated changelog, release notes, and plugin versions. On refresh, generate in a disposable checkout at the updated source base, then replace only the target release artifacts; preserve historical entries and reviewed compatibility notes. Do not run the branch-creating script on an existing release branch.
4. Write the required customer communications below **in the preparation PR**, not after approval. Refresh them when the source range changes.
5. Run the repository PR checklist, including base sync and `GCX_AGENT_MODE=false mise run all`; inspect generated-file drift. If sync adds source changes, regenerate/review before the final gates. Commit and push the reviewed release branch, then open/update its PR using the repository PR format. Any head change needs renewed review.

## Customer communications

Required for every major release, or minor release containing breaking changes:

- Use [grafana-product:whats-new](https://github.com/grafana/ai-kit/tree/main/plugins/grafana-product/skills/whats-new) for new or improved capabilities.
- Use [grafana-product:whats-next](https://github.com/grafana/ai-kit/tree/main/plugins/grafana-product/skills/whats-next) for breaking changes, deprecations, removals, or disruptive behaviour changes.
- When both apply, write separate drafts. Reuse existing contributions where appropriate.

Read each relevant upstream skill and required references, retrieving them with `gh` if not installed. Follow its content/formatting guidance, but override advance-notice timing: prepare during this release without requiring a 2–4 week delay. State actual availability, affected users, exact before/after behaviour, and concrete migrations. Verify CMS metadata; a gcx version is not a Grafana self-managed version.

Commit drafts under `docs/releases/<version>/` as `whats-new.md` and/or `whats-next.md`, linking them in the PR. For existing contributions, include their links and reviewed content/status in the PR. Record a comms owner and distinguish drafted, submitted, and published. Missing upstream guidance is a blocker to the required draft, not permission to invent CMS requirements. If no contribution is required, state why.

## Handoff

Return PR URL, version, current head SHA, source base SHA, previous tag/range, checks run, communications draft links/owner/status, and outstanding review. End: **ready for review; publication not started**. Link the separate [publish-release skill](../publish-release/SKILL.md).
