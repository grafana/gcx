---
name: release
description: Tag and release a new gcx version. Use when the user wants to cut a release, tag a version, run the release process, or says "release patch/minor/major".
---

# Releasing gcx

Automated via `mise run tag`. Requires `claude` CLI and [`svu`](https://github.com/caarlos0/svu).

```bash
mise run tag -- patch   # or minor, major
```

This generates a changelog entry (via Claude), updates `CHANGELOG.md` and `.release-notes.md`, commits on a `release/vX.Y.Z` branch, and pushes the branch. Then:

1. Open a PR and merge it (the script prints the exact command)
2. After merge, tag the commit on main and push the tag:
   ```bash
   git checkout main && git pull
   git tag v0.X.Y
   git push origin v0.X.Y
   ```

The tag push triggers the GoReleaser workflow.
