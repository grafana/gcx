#!/usr/bin/env bash
# scripts/tag.sh — bump version, verify changelog entry, commit, push release branch.
# The changelog entry is written before this runs — by the release skill
# (.claude/skills/release/SKILL.md) or by hand.
# Usage: bash scripts/tag.sh <major|minor|patch>
# Set DRY_RUN=1 to skip the git commit/push steps (used by tests).

set -euo pipefail

BUMP="${1:-}"

# ── validate args ─────────────────────────────────────────────────────────────

if [[ -z "$BUMP" ]]; then
	echo "Usage: mise run tag -- <major|minor|patch>" >&2
	exit 1
fi

case "$BUMP" in
major | minor | patch) ;;
*)
	echo "Error: invalid BUMP value '${BUMP}'. Must be major, minor, or patch." >&2
	exit 1
	;;
esac

# ── check dependencies ────────────────────────────────────────────────────────

if ! command -v svu >/dev/null 2>&1; then
	echo "Error: 'svu' is required but not found." >&2
	echo "Install with: go install github.com/caarlos0/svu/v3@latest" >&2
	exit 1
fi

# ── get latest tag ────────────────────────────────────────────────────────────

LAST_TAG=$(svu current 2>/dev/null || echo "v0.0.0")

# ── check for new commits since last tag ─────────────────────────────────────

if [[ "$LAST_TAG" == "v0.0.0" ]]; then
	COMMIT_COUNT=$(git rev-list --count HEAD 2>/dev/null || echo "0")
else
	COMMIT_COUNT=$(git rev-list --count "${LAST_TAG}..HEAD" 2>/dev/null || echo "0")
fi

if [[ "$COMMIT_COUNT" -eq 0 ]]; then
	echo "Error: no new commits since ${LAST_TAG}. Nothing to release." >&2
	exit 1
fi

# ── bump version ──────────────────────────────────────────────────────────────

case "$BUMP" in
major) NEW_TAG=$(svu major) ;;
minor) NEW_TAG=$(svu minor) ;;
patch) NEW_TAG=$(svu patch) ;;
esac

echo "Bumping ${LAST_TAG} → ${NEW_TAG}"

# ── verify changelog entry and release notes ─────────────────────────────────

CHANGELOG="CHANGELOG.md"
FIRST_DOCUMENTED=$(grep -m1 '^## v' "$CHANGELOG" 2>/dev/null |
	sed 's/^## \(v[^ )]*\).*/\1/' || true)

if [[ "$FIRST_DOCUMENTED" != "$NEW_TAG" ]]; then
	echo "Error: ${CHANGELOG} does not start with an entry for ${NEW_TAG} (found: ${FIRST_DOCUMENTED:-none})." >&2
	echo "Write the changelog entry first — see .claude/skills/release/SKILL.md." >&2
	exit 1
fi

if [[ ! -s .release-notes.md ]]; then
	echo "Error: .release-notes.md is missing or empty." >&2
	echo "Write it first — see .claude/skills/release/SKILL.md." >&2
	exit 1
fi

echo "Verified ${CHANGELOG} entry and .release-notes.md for ${NEW_TAG}"

# ── bump Claude plugin version ──────────────────────────────────────────────

SEMVER="${NEW_TAG#v}"
PLUGIN_JSON="claude-plugin/.claude-plugin/plugin.json"
MARKETPLACE_JSON=".claude-plugin/marketplace.json"

for f in "$PLUGIN_JSON" "$MARKETPLACE_JSON"; do
	if [[ -f "$f" ]]; then
		sed -i.bak 's/"version": "[^"]*"/"version": "'"${SEMVER}"'"/' "$f" && rm -f "${f}.bak"
		echo "Updated plugin version in ${f} → ${SEMVER}"
	fi
done

# ── dry-run exits here ────────────────────────────────────────────────────────

if [[ "${DRY_RUN:-0}" == "1" ]]; then
	echo "[DRY_RUN] Would commit, tag ${NEW_TAG}, and push."
	exit 0
fi

# ── commit on release branch, push ───────────────────────────────────────────

RELEASE_BRANCH="release/${NEW_TAG}"
git checkout -b "$RELEASE_BRANCH"

git add "$CHANGELOG" .release-notes.md
[[ -f "$PLUGIN_JSON" ]] && git add "$PLUGIN_JSON"
[[ -f "$MARKETPLACE_JSON" ]] && git add "$MARKETPLACE_JSON"
git commit -m "chore(release): ${NEW_TAG} changelog"

echo "Pushing branch ${RELEASE_BRANCH}..."
git push -u origin "$RELEASE_BRANCH"

echo ""
echo "Branch ${RELEASE_BRANCH} pushed."
echo ""
echo "Next steps:"
echo "  1. Open a PR and merge it:"
echo "       gh pr create --title 'chore(release): ${NEW_TAG} changelog' --body 'Release ${NEW_TAG}'"
echo "  2. After merge, tag the commit on main and push the tag:"
echo "       git checkout main && git pull"
echo "       git tag ${NEW_TAG}"
echo "       git push origin ${NEW_TAG}"
echo ""
echo "The tag push triggers GoReleaser."
