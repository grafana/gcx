---
name: release
description: Tag and release a new gcx version. Use when the user wants to cut a release, tag a version, run the release process, or says "release patch/minor/major".
argument-hint: <major|minor|patch>
---

# Releasing gcx

Requires [`svu`](https://github.com/caarlos0/svu). You write the changelog entry directly, then `mise run tag` verifies it and does the mechanical steps. Do NOT create tags in this flow — tagging happens after the release PR merges.

## Step 1: Determine the version

```bash
git checkout main && git pull
svu current                    # LAST_TAG
svu <major|minor|patch>        # NEW_TAG
date -u +%Y-%m-%d              # entry date
```

## Step 2: Write the changelog entry

Gather the commits:

```bash
git log --no-merges --format='%s' <LAST_TAG>..HEAD
```

Prepend a new entry to `CHANGELOG.md`:

```markdown
## vX.Y.Z (YYYY-MM-DD)

### Breaking changes

- ...

### Features

- scope: description

### Fixes

- scope: description

### Docs

- ...
```

Rules:

- Categorize by conventional-commit prefix: `feat` → Features, `fix` → Fixes, `docs` → Docs. Breaking changes (`!:` suffix or `BREAKING CHANGE`) go in a Breaking changes section first.
- Skip `chore(deps)`, `ci`, `test`, and other non-user-facing commits (including the release commits themselves).
- Group related commits into one bullet. Plain English, under 80 chars per bullet.
- Only include sections that have entries.
- Section order: Breaking changes, Features, Fixes, Docs.
- If the newest `## v` entry in `CHANGELOG.md` is older than `svu current`, backfill an entry for each missing tag first (commits from `<prev-tag>..<tag>`, dated `git log -1 --format=%as <tag>`), newest first.

Then write `.release-notes.md`: the new entry's body only, without the `## vX.Y.Z (date)` header line. GoReleaser uses it for the GitHub release body.

Show the user the new entry before continuing.

## Step 3: Commit and push

```bash
mise run tag -- <major|minor|patch>
```

This verifies `CHANGELOG.md` starts with the new version's entry, bumps the plugin versions, commits on a `release/vX.Y.Z` branch, and pushes the branch.

## Step 4: PR and tag

1. Open a PR and merge it (the script prints the exact command)
2. After merge, tag the commit on main and push the tag:
   ```bash
   git checkout main && git pull
   git tag v0.X.Y
   git push origin v0.X.Y
   ```

The tag push triggers the GoReleaser workflow.
