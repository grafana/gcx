# Changelog generation and refresh

Read `scripts/tag.sh` at the source revision before use. Its dry-run still edits files and invokes Claude; it skips only branch creation, commit, and push. It prepends generated text and can backfill old tags. It is not an idempotent refresh command.

## Freeze the input

- Record the prior release tag and source commit, with the tag an ancestor of that commit. Confirm the generator's `svu current` and requested bump select that same range and version.
- For a new release, use the fetched upstream base commit. For an existing PR, inspect its diff and sync its base first; use that base without the old generated release commit as generation input.
- Preserve the existing release branch and reviewed text. Use a disposable checkout/clone with the required history and tags for generation on refresh. Copy back only the intended version's artifacts, not an entire regenerated historical changelog.
- If the release PR contains non-release code, or later releases make the requested range/version ambiguous, stop for a scope decision. Do not silently fold source edits into the release metadata or change the version.

## Review output

The release PR contains:

- `CHANGELOG.md`: `## Unreleased` first, then exactly one target version section; prior sections preserved.
- `.release-notes.md`: the reviewed target section body, without the version heading.
- `claude-plugin/.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json`: matching intended semantic version (without `v`).
- Required What's New/Next writing and its handoff, as described in the preparation skill.

Remove enclosing model code fences, preambles, and meta-commentary. Verify every cited PR belongs to the source range, not an earlier release. Inspect the actual source PRs for claims that commit subjects cannot establish. Preserve reviewed deprecation and migration caveats; patch numbering is not proof that behaviour is unchanged. Do not introduce unrelated historical backfills without review.

After refresh, compare both old and new target sections: new source changes appear once, historical sections remain unchanged, and previously reviewed caveats have not disappeared. The PR description carries the new source range and head SHA; previous approval does not cover the new head.
