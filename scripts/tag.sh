#!/usr/bin/env bash
# scripts/tag.sh — bump version, generate AI changelog entry, commit, tag, push.
# Usage: bash scripts/tag.sh <major|minor|patch>
# Set DRY_RUN=1 to skip the git commit/tag/push steps (used by tests).

set -euo pipefail

BUMP="${1:-}"

# ── validate args ─────────────────────────────────────────────────────────────

if [[ -z "$BUMP" ]]; then
  echo "Usage: make tag BUMP=<major|minor|patch>" >&2
  exit 1
fi

case "$BUMP" in
  major|minor|patch) ;;
  *)
    echo "Error: invalid BUMP value '${BUMP}'. Must be major, minor, or patch." >&2
    exit 1
    ;;
esac

# ── check dependencies ────────────────────────────────────────────────────────

if ! command -v claude >/dev/null 2>&1; then
  echo "Error: 'claude' CLI is required but not found. Install it from https://claude.ai/download" >&2
  exit 1
fi

# ── get latest tag ────────────────────────────────────────────────────────────

LAST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")

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

VERSION="${LAST_TAG#v}"
IFS='.' read -r MAJOR MINOR PATCH <<< "$VERSION"
MAJOR=${MAJOR:-0}
MINOR=${MINOR:-0}
PATCH=${PATCH:-0}

case "$BUMP" in
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  patch) PATCH=$((PATCH + 1)) ;;
esac

NEW_TAG="v${MAJOR}.${MINOR}.${PATCH}"
TODAY=$(date -u +%Y-%m-%d)

echo "Bumping ${LAST_TAG} → ${NEW_TAG}"

# ── helper: generate one changelog section ───────────────────────────────────
# Usage: generate_entry <from_ref> <to_ref> <display_tag> <date>
# Prints the "## vX.Y.Z (date)\n\n<bullets>\n" block, or nothing if no commits.

generate_entry() {
  local from_ref="$1" to_ref="$2" display_tag="$3" entry_date="$4"
  local commits diffstat

  if [[ "$from_ref" == "v0.0.0" ]]; then
    commits=$(git log --oneline "$to_ref" 2>/dev/null || echo "")
    diffstat=$(git diff --stat "$(git rev-list --max-parents=0 HEAD)" "$to_ref" 2>/dev/null || echo "")
  else
    commits=$(git log --oneline "${from_ref}..${to_ref}" 2>/dev/null || echo "")
    diffstat=$(git diff --stat "${from_ref}" "${to_ref}" 2>/dev/null || echo "")
  fi

  [[ -z "$commits" ]] && return 0

  local prompt="You are writing a CHANGELOG entry for a CLI tool called gcx (Grafana Cloud resource manager).

Summarize the following commits into a concise bullet-point list for version ${display_tag}.
Group related changes. Use plain English. Keep each bullet under 80 chars.
Do not include version header — just the bullets.

Commits:
${commits}

Diffstat:
${diffstat}"

  local summary
  echo "Generating changelog entry for ${display_tag} with Claude..." >&2
  summary=$(echo "$prompt" | env -u CLAUDECODE claude -p 2>/dev/null)

  printf '## %s (%s)\n\n%s\n' "$display_tag" "$entry_date" "$summary"
}

# ── detect last documented version in CHANGELOG.md ───────────────────────────

CHANGELOG="CHANGELOG.md"
LAST_CHANGELOG_VERSION=$(grep -m1 '^## v' "$CHANGELOG" 2>/dev/null \
  | sed 's/^## \(v[^ )]*\).*/\1/' || echo "v0.0.0")

# ── backfill any tags not yet documented ─────────────────────────────────────

NEW_CONTENT=""

if [[ "$LAST_CHANGELOG_VERSION" != "$LAST_TAG" ]]; then
  PREV="$LAST_CHANGELOG_VERSION"
  while IFS= read -r GAP_TAG; do
    GAP_DATE=$(git log -1 --format="%as" "$GAP_TAG")
    entry=$(generate_entry "$PREV" "$GAP_TAG" "$GAP_TAG" "$GAP_DATE")
    if [[ -n "$entry" ]]; then
      NEW_CONTENT="${NEW_CONTENT}${entry}

"
    fi
    PREV="$GAP_TAG"
  done < <(git tag --sort=version:refname | awk -v start="$LAST_CHANGELOG_VERSION" \
    '$0 == start { found=1; next } found { print }')
fi

# ── generate the new version entry ───────────────────────────────────────────

NEW_ENTRY=$(generate_entry "$LAST_TAG" "HEAD" "$NEW_TAG" "$TODAY")
NEW_CONTENT="${NEW_ENTRY}

${NEW_CONTENT}"

# ── write changelog ───────────────────────────────────────────────────────────

if [[ -f "$CHANGELOG" ]]; then
  EXISTING=$(cat "$CHANGELOG")
  printf '%s\n%s\n' "$NEW_CONTENT" "$EXISTING" > "$CHANGELOG"
else
  printf '%s\n' "$NEW_CONTENT" > "$CHANGELOG"
fi

echo "Updated ${CHANGELOG}"

# ── write release notes (used by GoReleaser for GitHub release body) ──────────
# Strip the "## vX.Y.Z (date)" header line and the blank line after it.

printf '%s\n' "$NEW_ENTRY" | tail -n +3 > .release-notes.md
echo "Updated .release-notes.md"

# ── dry-run exits here ────────────────────────────────────────────────────────

if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "[DRY_RUN] Would commit, tag ${NEW_TAG}, and push."
  exit 0
fi

# ── commit, tag, push ─────────────────────────────────────────────────────────

git add "$CHANGELOG" .release-notes.md
git commit -m "chore(release): ${NEW_TAG} changelog"
git tag "$NEW_TAG"

echo "Pushing commit and tag ${NEW_TAG}..."
git push
git push origin "$NEW_TAG"

echo "Done. Tag ${NEW_TAG} pushed — GoReleaser will take it from here."
