# Conventional Commits via PR Title Enforcement

**Created**: 2026-03-27
**Status**: accepted
**Bead**: none
**Supersedes**: none

## Context

The gcx repo already follows Conventional Commits informally — recent history
shows consistent use of `feat(scope):`, `fix:`, `docs:`, `ci:`, `chore:`,
`build(deps):`. The goal is to codify and encourage the convention without
blocking contributors unnecessarily.

The repo is **squash-merge-only** with `PR_TITLE` as the squash commit title
and `PR_BODY` as the commit body. Individual commits within a PR are discarded
at merge time. This means PR title lint is the single meaningful enforcement
point.

Primary motivation is readability/consistency of the git log. Automated
changelogs are a future option but not a driver today.

## Decision

Adopt a layered approach — four independent components that reinforce each
other, all Go/shell-only (no Node.js dependencies):

### 1. PR Title Lint (CI)

A GitHub Actions workflow on `pull_request` events (`opened`, `edited`,
`synchronize`) that checks the PR title against the Conventional Commits regex:

```
^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(.+\))?!?: .+
```

Two modes, controlled by a GitHub Actions variable `CC_ENFORCE`:
- **`warn`** (default): Posts a warning annotation if the title doesn't match.
  Does not block merge.
- **`require`**: Fails the status check, blocks merge.

Graduation path: flip `CC_ENFORCE` from `warn` to `require` when the team is
comfortable.

### 2. PR Template

`.github/PULL_REQUEST_TEMPLATE.md` with a CC format reminder at the top and
standard sections (Summary, Test plan).

### 3. Git Commit Template

`.gitmessage` at the repo root with CC format hints. A `make setup` target
configures it for the local clone via `git config commit.template .gitmessage`.

### 4. CLAUDE.md Convention Entry

Add a convention entry so Claude Code and other AI agents format PR titles
and commit messages using Conventional Commits.

## Allowed Types

| Type | Use for |
|------|---------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `style` | Formatting, no logic change |
| `refactor` | Code restructuring, no behavior change |
| `perf` | Performance improvement |
| `test` | Adding/updating tests |
| `build` | Build system, dependencies |
| `ci` | CI/CD configuration |
| `chore` | Maintenance, tooling |
| `revert` | Reverting a previous commit |

Scope is optional but encouraged (e.g. `feat(slo):`, `fix(providers):`).

## Alternatives Considered

- **Git commit-msg hook**: Requires per-contributor setup, doesn't help agents
  or GitHub web UI, bypassable with `--no-verify`. Irrelevant when squash-merge
  discards individual commits.
- **Full CI commit lint (every commit)**: Overkill when squash-merge makes only
  the PR title matter. Annoying for WIP `fixup!` commits during development.
- **Node.js commitlint**: Gold standard but adds a Node.js dependency to a
  Go-only toolchain.

## Consequences

- PR titles become the single source of truth for commit messages.
- The warn→require graduation path lets the team adopt incrementally.
- Future automated changelog generation becomes straightforward since all
  squash commits will follow CC format.
