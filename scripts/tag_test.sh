#!/usr/bin/env bash
# Tests for scripts/tag.sh
# Run with: bash scripts/tag_test.sh

set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/tag.sh"
PASS=0
FAIL=0

# ── helpers ──────────────────────────────────────────────────────────────────

green() { printf '\033[0;32m✓ %s\033[0m\n' "$*"; }
red()   { printf '\033[0;31m✗ %s\033[0m\n' "$*"; }

pass() { green "$1"; PASS=$((PASS + 1)); }
fail() { red   "$1"; FAIL=$((FAIL + 1)); }

# Create a temp git repo with some commits and optional tag.
make_repo() {
  local dir
  dir=$(mktemp -d)
  git -C "$dir" init -q
  git -C "$dir" config user.email "test@test.com"
  git -C "$dir" config user.name "Test"
  echo "# repo" > "$dir/README.md"
  git -C "$dir" add .
  git -C "$dir" commit -q -m "chore: initial commit"
  echo "$dir"
}

add_commit() {
  local dir=$1 msg=$2
  echo "$RANDOM" >> "$dir/README.md"
  git -C "$dir" add .
  git -C "$dir" commit -q -m "$msg"
}

add_tag() {
  local dir=$1 tag=$2
  git -C "$dir" tag "$tag"
}

mock_tools() {
  local dir
  dir=$(mktemp -d)
  printf '#!/bin/sh\necho "- mocked entry"\n' > "$dir/claude"
  chmod +x "$dir/claude"
  # svu mock that delegates to real svu behavior via git tags
  cat > "$dir/svu" <<'SVUSCRIPT'
#!/bin/sh
case "$1" in
  current) git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0" ;;
  major)
    v=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    v="${v#v}"; IFS='.' read -r M m p <<EOF
$v
EOF
    echo "v$((M + 1)).0.0" ;;
  minor)
    v=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    v="${v#v}"; IFS='.' read -r M m p <<EOF
$v
EOF
    echo "v${M}.$((m + 1)).0" ;;
  patch)
    v=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
    v="${v#v}"; IFS='.' read -r M m p <<EOF
$v
EOF
    echo "v${M}.${m}.$((p + 1))" ;;
esac
SVUSCRIPT
  chmod +x "$dir/svu"
  echo "$dir"
}

# ── version bumping tests ────────────────────────────────────────────────────

test_bump_patch() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.5.5"
  add_commit "$dir" "fix: some fix"
  mock=$(mock_tools)

  local out
  out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1)
  if echo "$out" | grep -q "v0.5.6"; then
    pass "patch bump: v0.5.5 → v0.5.6"
  else
    fail "patch bump: v0.5.5 → v0.5.6 (got: $out)"
  fi
  rm -rf "$dir" "$mock"
}

test_bump_minor() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.5.5"
  add_commit "$dir" "feat: new feature"
  mock=$(mock_tools)

  local out
  out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" minor 2>&1)
  if echo "$out" | grep -q "v0.6.0"; then
    pass "minor bump: v0.5.5 → v0.6.0"
  else
    fail "minor bump: v0.5.5 → v0.6.0 (got: $out)"
  fi
  rm -rf "$dir" "$mock"
}

test_bump_major() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.5.5"
  add_commit "$dir" "feat!: breaking change"
  mock=$(mock_tools)

  local out
  out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" major 2>&1)
  if echo "$out" | grep -q "v1.0.0"; then
    pass "major bump: v0.5.5 → v1.0.0"
  else
    fail "major bump: v0.5.5 → v1.0.0 (got: $out)"
  fi
  rm -rf "$dir" "$mock"
}

test_fallback_no_tags() {
  local dir mock
  dir=$(make_repo)
  add_commit "$dir" "feat: first feature"
  mock=$(mock_tools)

  local out
  out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1)
  if echo "$out" | grep -q "v0.0.1"; then
    pass "no tags fallback: v0.0.0 → v0.0.1"
  else
    fail "no tags fallback: v0.0.0 → v0.0.1 (got: $out)"
  fi
  rm -rf "$dir" "$mock"
}

# ── error handling tests ──────────────────────────────────────────────────────

test_no_bump_arg() {
  local dir
  dir=$(make_repo)
  local rc=0 out
  out=$(cd "$dir" && bash "$SCRIPT" 2>&1) || rc=$?
  if [[ $rc -ne 0 ]] && echo "$out" | grep -qi "usage\|bump\|major\|minor\|patch"; then
    pass "no BUMP arg → usage error"
  else
    fail "no BUMP arg → usage error (rc=$rc, got: $out)"
  fi
  rm -rf "$dir"
}

test_invalid_bump() {
  local dir
  dir=$(make_repo)
  local rc=0 out
  out=$(cd "$dir" && bash "$SCRIPT" bogus 2>&1) || rc=$?
  if [[ $rc -ne 0 ]] && echo "$out" | grep -qi "invalid\|bogus\|major\|minor\|patch"; then
    pass "invalid BUMP → error"
  else
    fail "invalid BUMP → error (rc=$rc, got: $out)"
  fi
  rm -rf "$dir"
}

test_claude_not_installed() {
  local dir
  dir=$(make_repo)
  add_commit "$dir" "feat: something"
  add_tag "$dir" "v0.1.0"
  add_commit "$dir" "feat: another thing"

  local empty_path
  empty_path=$(mktemp -d)

  local rc=0 out
  out=$(cd "$dir" && PATH="$empty_path" "$(command -v bash)" "$SCRIPT" patch 2>&1) || rc=$?
  if [[ $rc -ne 0 ]] && echo "$out" | grep -qi "claude"; then
    pass "claude not installed → clear error"
  else
    fail "claude not installed → clear error (rc=$rc, got: $out)"
  fi
  rm -rf "$dir" "$empty_path"
}

test_no_new_commits() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.1.0"
  mock=$(mock_tools)

  local rc=0 out
  out=$(cd "$dir" && PATH="$mock:$PATH" bash "$SCRIPT" patch 2>&1) || rc=$?
  if [[ $rc -ne 0 ]] && echo "$out" | grep -qi "no new commits\|nothing to release\|no commits"; then
    pass "no new commits → error"
  else
    fail "no new commits → error (rc=$rc, got: $out)"
  fi
  rm -rf "$dir" "$mock"
}

# ── changelog tests ───────────────────────────────────────────────────────────

test_changelog_created_if_missing() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.2.0"
  add_commit "$dir" "feat: add thing"
  mock=$(mock_tools)

  (cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || true

  if [[ -f "$dir/CHANGELOG.md" ]]; then
    pass "CHANGELOG.md created when missing"
  else
    fail "CHANGELOG.md created when missing"
  fi
  rm -rf "$dir" "$mock"
}

test_changelog_prepended_if_exists() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.2.0"
  add_commit "$dir" "feat: new thing"

  echo "## v0.2.0 (2025-01-01)" > "$dir/CHANGELOG.md"
  echo "- old entry" >> "$dir/CHANGELOG.md"

  mock=$(mock_tools)

  (cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || true

  local first_line
  first_line=$(head -1 "$dir/CHANGELOG.md")
  if echo "$first_line" | grep -q "v0.2.1"; then
    pass "CHANGELOG.md prepended (new version first)"
  else
    fail "CHANGELOG.md prepended (got first line: $first_line)"
  fi
  rm -rf "$dir" "$mock"
}

test_changelog_header_format() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v1.2.3"
  add_commit "$dir" "fix: something"
  mock=$(mock_tools)

  (cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || true

  if grep -q "## v1.2.4" "$dir/CHANGELOG.md" 2>/dev/null; then
    pass "CHANGELOG.md header format: ## vX.Y.Z (date)"
  else
    fail "CHANGELOG.md header format (content: $(cat "$dir/CHANGELOG.md" 2>/dev/null || echo 'missing'))"
  fi
  rm -rf "$dir" "$mock"
}

# ── release notes test ───────────────────────────────────────────────────────

test_release_notes_written() {
  local dir mock
  dir=$(make_repo)
  add_tag "$dir" "v0.3.0"
  add_commit "$dir" "feat: something great"
  mock=$(mock_tools)

  (cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || true

  if [[ -f "$dir/.release-notes.md" ]]; then
    local has_bullet has_header
    grep -q "mocked entry" "$dir/.release-notes.md" && has_bullet=1 || has_bullet=0
    grep -q "^## " "$dir/.release-notes.md" && has_header=1 || has_header=0
    if [[ $has_bullet -eq 1 && $has_header -eq 0 ]]; then
      pass ".release-notes.md has bullets, no header"
    else
      fail ".release-notes.md content wrong (bullet=$has_bullet, header=$has_header): $(cat "$dir/.release-notes.md")"
    fi
  else
    fail ".release-notes.md not created"
  fi
  rm -rf "$dir" "$mock"
}

# ── backfill gap test ─────────────────────────────────────────────────────────

test_backfill_gap() {
  local dir mock
  dir=$(make_repo)

  add_commit "$dir" "feat: initial feature"
  add_tag "$dir" "v0.1.0"
  printf '## v0.1.0 (2025-01-01)\n\n- initial feature\n' > "$dir/CHANGELOG.md"

  add_commit "$dir" "feat: gap feature"
  add_tag "$dir" "v0.2.0"

  add_commit "$dir" "fix: new fix"
  mock=$(mock_tools)

  (cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || true

  local has_gap has_new
  grep -q "## v0.2.0" "$dir/CHANGELOG.md" 2>/dev/null && has_gap=1 || has_gap=0
  grep -q "## v0.2.1" "$dir/CHANGELOG.md" 2>/dev/null && has_new=1 || has_new=0

  if [[ $has_gap -eq 1 && $has_new -eq 1 ]]; then
    local line_gap line_new
    line_gap=$(grep -n "## v0.2.0" "$dir/CHANGELOG.md" | head -1 | cut -d: -f1)
    line_new=$(grep -n "## v0.2.1" "$dir/CHANGELOG.md" | head -1 | cut -d: -f1)
    if [[ $line_new -lt $line_gap ]]; then
      pass "backfill gap: v0.2.0 backfilled and v0.2.1 prepended in correct order"
    else
      fail "backfill gap: entries out of order (v0.2.1 line=$line_new, v0.2.0 line=$line_gap)"
    fi
  else
    fail "backfill gap: missing entries (v0.2.0 present=$has_gap, v0.2.1 present=$has_new)"
  fi
  rm -rf "$dir" "$mock"
}

# ── run all ───────────────────────────────────────────────────────────────────

echo "Running tag.sh tests..."
echo

test_no_bump_arg
test_invalid_bump
test_claude_not_installed
test_no_new_commits
test_bump_patch
test_bump_minor
test_bump_major
test_fallback_no_tags
test_changelog_created_if_missing
test_changelog_prepended_if_exists
test_changelog_header_format
test_release_notes_written
test_backfill_gap

echo
echo "Results: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
