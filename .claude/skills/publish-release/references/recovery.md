# Exact-commit publication and recovery

The task handoff is evidence, not authoritative state. Re-read GitHub state before each mutation. Use explicit repositories and identifiers; never infer a tag target from the checkout's HEAD or a release's `target_commitish` string alone.

## Tag target

Resolve the release PR's `mergeCommit.oid` with `gh pr view`, fetch the commit, and verify it is the intended merged release. Inspect local `refs/tags/<tag>` and remote `git ls-remote --tags origin` results, including peeled annotated tags. Compare **commit targets**, not annotated-tag object SHAs. A remote read failure is unknown state, not an absent tag. Fetch a remote tag without force only after checking local consistency.

For a missing tag, the mutation is `git tag <tag> <merge-sha>` followed by `git push origin refs/tags/<tag>`. Verify the remote target afterward. Never use `git tag <tag>` with an implicit HEAD, `--force`, a deletion, or a broad `--tags` push. If another actor creates the tag concurrently, re-read its target and reconcile; never overwrite it.

## Resume decisions

| Verified state | Next action |
|---|---|
| PR open, approved head unchanged, checks green | Merge with expected-head guard; resolve actual merge SHA |
| PR head changed or review/checks missing | Stop before merge; obtain review/checks for this head |
| PR merged, main advanced, tag absent | Tag the PR merge SHA, not new main |
| Matching local tag, remote absent | Push only that tag and verify remote |
| Matching remote tag, workflow running | Reuse tag; follow the matching run |
| Local or remote tag targets another commit | Stop and report both targets; never move it |
| GitHub release published, tap pending | Verify release/assets, continue tap review |
| Tap merged, Slack pending | Verify formula, communications and core status; reconcile Slack before sending |
| All stages already complete | Return existing links; do not repeat mutations |

Inspect `.github/workflows/release.yaml` and `.goreleaser.yaml` at the tag for expected behavior. The current configuration builds linux/darwin/windows on amd64/arm64, tar.gz except Windows zip, plus a SHA-256 checksum file. Derive actual filenames and supported platforms from the tagged config; do not hardcode this list forever. Verify the release body matches the reviewed notes and the checksum file covers the expected archives. Download and hash assets when verifying integrity; a checksum filename alone proves nothing.

On a failed/cancelled/missing run, inspect logs, tag, release state, and existing assets. An absent release and pre-publication failure may permit retry of the same run after the cause is resolved. Changing release infrastructure to resolve that cause needs separate authorization. If GoReleaser partially uploaded/published, stop with the observed state and a recovery proposal; do not blindly rerun, delete a release, or replace assets. Never create a new tag to work around a workflow failure.

Record version, PR/head/merge SHA, tag commit, workflow/release links, tap/core states, communications owner/status, and Slack permalink in the handoff. Keep pending work explicit if access or review permissions block continuation.
