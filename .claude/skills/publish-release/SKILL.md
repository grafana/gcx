---
name: publish-release
description: Use when a maintainer explicitly requests publication of an approved gcx release PR, or resumption of its unfinished GitHub release, Homebrew tap, communications handoff, or Slack announcement.
---

# Publish a gcx release

Consume an approved release PR, not just a version number. An explicit request to publish authorizes merging that PR, tagging/pushing its merge commit, reviewing/approving/merging its Grafana tap PR, and posting the release announcement to `#gcx`. Respect narrower user limits. Discovering this skill or finding an approval is not publication authorization. Never bypass protections or move a release tag.

## Verify and publish

1. Resolve the PR in `grafana/gcx`. Verify its base, intended version, release artifacts, and required communications drafts. For an open PR, verify current head, current approval, and successful required checks; for a merged PR, inspect merged artifacts and merge evidence without requiring new approval. Inspect the actual diff and [artifact requirements](../prepare-release/references/changelog-review.md). Approval of an older head is insufficient. Missing required drafts return to preparation/review; do not invent them after approval.
2. Merge the open PR using the repository's allowed merge method and `gh pr merge --match-head-commit <reviewed-head>` with explicit repository/PR. Recheck if its head changes. Wait for actual merge if queued. Resolve `mergeCommit.oid` from the merged PR, including on resume. For an already merged PR, inspect the merged artifacts and merge evidence; do not demand a new approval on a closed PR.
3. Read [recovery](references/recovery.md) before tagging. Verify local and remote tag targets. Create the intended tag at the PR's **exact merge SHA**, never the current `main` tip, and push only that tag. Reuse matching tags. A conflicting tag is a blocker, not a reason to force-update.
4. Find `.github/workflows/release.yaml` runs for this tag and merge SHA. Wait in bounded intervals with progress updates. Verify a successful GoReleaser run and a published, non-draft GitHub release with correct notes and assets from `.goreleaser.yaml` at that commit. For a stable version, also verify it is not marked prerelease. Check assets and checksum contents, not merely release existence. Inspect failure/partial publication before retrying.
5. Read [distribution and announcement](references/distribution-and-announcement.md). Review, approve, and merge the generated Grafana tap PR and verify its default-branch formula. Check Homebrew core separately; it is not the tap. Complete the required communications handoff, then post one accurate `#gcx` release announcement.

The tap workflow starts independently on the tag push. Inspect both workflows while waiting, but verify GitHub publication before announcing availability. Core approval/merge is outside this skill's authority.

## Completion report

Return version, release PR/head/merge SHA, verified tag target, workflow and GitHub release links, tap PR and verified formula status, separate core status, communications draft/submission/publication links and owner, and Slack permalink. Link this skill. Mark blocked or pending steps explicitly; a published binary alone is not completion of this workflow. On resume, verify live state and perform only unfinished steps.
