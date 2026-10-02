# Distribution and announcement

## Grafana Homebrew tap

Read `.github/workflows/publish-homebrew-formula.yml` and `.github/homebrew/gcx.rb.tmpl` at the release revision. Tag push starts this independently of GoReleaser. Prerelease tags are intentionally skipped; report tap not applicable rather than manufacturing a formula PR.

1. Find the matching `gcx-<version>` head in `grafana/homebrew-grafana`, searching all PR states. Also inspect the tap's current default branch and root `gcx.rb`. If already merged with the intended source/checksum, reuse that evidence.
2. If absent, inspect the matching workflow/run before dispatching a retry with the intended tag. A dispatch uses the template at the dispatch ref: verify that template and inputs. The workflow can reset the generated branch; do not blindly rerun over reviewed changes. It searches PRs in **all states** and does not recreate a closed, unmerged PR. For that state, stop with the existing PR and needed maintainer resolution rather than reporting success or creating duplicates.
3. Read the **whole PR diff** at its current head. Verify only intended `gcx.rb` changes, the exact `grafana/gcx` tag archive URL, SHA-256 independently calculated from that archive, version/build metadata and completion/test behavior against the template. Unexpected changes require investigation, not automatic approval.
4. Verify current required checks and mergeability. Submit approval tied to the reviewed head, e.g. GitHub's pull-request reviews API with `event=APPROVE` and `commit_id=<reviewed-head>`. Use the authenticated maintainer identity. Follow repository comment/review formatting. Re-read head and review state, then merge with `gh pr merge --match-head-commit <reviewed-head>` and the allowed method. Do not use admin bypass. If self-approval or permissions prevent this, report the exact reviewer/maintainer action needed.
5. If head changes at any point, re-review/recheck the new head before approval/merge. Wait for actual merge, then verify `gcx.rb` on the default branch contains the intended URL, checksum and metadata. A green workflow or open PR alone does not mean the tap is updated.

If a newer version has superseded this release on the default branch, do not downgrade it. Verify the historical matching merged update and report the current newer version distinctly. If provenance is unclear, report the discrepancy.

## Homebrew core is separate

Find the version's PR in `Homebrew/homebrew-core` and inspect its default-branch `Formula/g/gcx.rb`. Report the observed version and PR/check state separately from the Grafana tap. `brew install gcx` uses core; `brew install grafana/grafana/gcx` uses the tap. An updated tap does not establish availability through core. Do not create duplicate core PRs or approve/merge them under this skill. Core pending does not block announcement; describe that status accurately.

## Customer communications handoff

Use the What's New/Next content reviewed in the preparation PR. Verify actual availability and metadata before submission. Follow the applicable upstream contribution workflow within the maintainer's authorization; obtain direction before additional external publication not covered by that authorization. Preserve the preparation skill's policy and timing override. Report each contribution as drafted, submitted, or published, with evidence. If access/approval prevents submission, name the owner and remaining action; do not equate a local draft with submission or claim communications complete. A reviewed draft with an explicit owner and remaining submission action satisfies the handoff for Slack; CMS and overall completion remain qualified as pending.

## Slack release announcement

Use the Slack connector to resolve `#gcx`, then read recent release posts and search the exact version/release URL. Read candidate threads to distinguish a top-level release announcement from a support-thread mention. Reuse a verified existing announcement and permalink on resume. Inspect any saved draft before sending; a draft is not a sent message.

Compose a short channel post with the GitHub release link, a few verified user-facing highlights, important migrations/deprecations, contributor thanks, and accurate distribution status. Use recent channel tone; do not promise `brew upgrade` availability unless the relevant formula is verified. Do not omit core-pending status while suggesting unqualified Homebrew availability.

After GitHub publication and the tap step are verified (or tap is intentionally inapplicable), send the announcement as part of the explicitly authorized publication request. If the tap is blocked, retain the draft and report the blocker instead of silently skipping that step. Pending core/CMS work can be disclosed without claiming it complete.

If Slack is unavailable, preserve a ready-to-post draft and mark announcement outstanding. If the send response is uncertain, reconcile channel history for the exact announcement before any retry. If history cannot establish whether it sent, stop and report uncertainty; do not risk a duplicate. On success, return the message permalink.
