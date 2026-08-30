#!/usr/bin/env bash
# Tests for scripts/tag.sh
# Run with: bash scripts/tag_test.sh

set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/tag.sh"
PASS=0
FAIL=0

# ── helpers ──────────────────────────────────────────────────────────────────

green() { printf '\033[0;32m✓ %s\033[0m\n' "$*"; }
red() { printf '\033[0;31m✗ %s\033[0m\n' "$*"; }

pass() {
	green "$1"
	PASS=$((PASS + 1))
}
fail() {
	red "$1"
	FAIL=$((FAIL + 1))
}

# Create a temp git repo with some commits and optional tag.
make_repo() {
	local dir
	dir=$(mktemp -d)
	git -C "$dir" init -q
	git -C "$dir" config user.email "test@test.com"
	git -C "$dir" config user.name "Test"
	git -C "$dir" config commit.gpgsign false
	git -C "$dir" config tag.gpgsign false
	echo "# repo" >"$dir/README.md"
	git -C "$dir" add .
	git -C "$dir" commit -q -m "chore: initial commit"
	echo "$dir"
}

add_commit() {
	local dir=$1 msg=$2
	echo "$RANDOM" >>"$dir/README.md"
	git -C "$dir" add .
	git -C "$dir" commit -q -m "$msg"
}

add_tag() {
	local dir=$1 tag=$2
	git -C "$dir" tag "$tag"
}

# Write the changelog entry and release notes for a version, as the release
# skill would before tag.sh runs.
write_release_files() {
	local dir=$1 version=$2
	local existing=""
	[[ -f "$dir/CHANGELOG.md" ]] && existing=$(cat "$dir/CHANGELOG.md")
	printf '## %s (2025-01-01)\n\n- test entry\n\n%s' "$version" "$existing" >"$dir/CHANGELOG.md"
	printf -- '- test entry\n' >"$dir/.release-notes.md"
}

mock_tools() {
	local dir
	dir=$(mktemp -d)
	# svu mock that delegates to real svu behavior via git tags
	cat >"$dir/svu" <<'SVUSCRIPT'
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
	write_release_files "$dir" "v0.5.6"
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
	write_release_files "$dir" "v0.6.0"
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
	write_release_files "$dir" "v1.0.0"
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
	write_release_files "$dir" "v0.0.1"
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

test_svu_not_installed() {
	local dir
	dir=$(make_repo)
	add_commit "$dir" "feat: something"
	add_tag "$dir" "v0.1.0"
	add_commit "$dir" "feat: another thing"

	local empty_path
	empty_path=$(mktemp -d)

	local rc=0 out
	out=$(cd "$dir" && PATH="$empty_path" "$(command -v bash)" "$SCRIPT" patch 2>&1) || rc=$?
	if [[ $rc -ne 0 ]] && echo "$out" | grep -qi "svu"; then
		pass "svu not installed → clear error"
	else
		fail "svu not installed → clear error (rc=$rc, got: $out)"
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

# ── changelog verification tests ─────────────────────────────────────────────

test_missing_changelog_entry() {
	local dir mock
	dir=$(make_repo)
	add_tag "$dir" "v0.2.0"
	add_commit "$dir" "feat: add thing"
	mock=$(mock_tools)

	local rc=0 out
	out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || rc=$?
	if [[ $rc -ne 0 ]] && echo "$out" | grep -q "entry for v0.2.1"; then
		pass "missing changelog entry → error"
	else
		fail "missing changelog entry → error (rc=$rc, got: $out)"
	fi
	rm -rf "$dir" "$mock"
}

test_stale_changelog_entry() {
	local dir mock
	dir=$(make_repo)
	add_tag "$dir" "v0.2.0"
	add_commit "$dir" "feat: new thing"

	printf '## v0.2.0 (2025-01-01)\n\n- old entry\n' >"$dir/CHANGELOG.md"
	printf -- '- old entry\n' >"$dir/.release-notes.md"
	mock=$(mock_tools)

	local rc=0 out
	out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || rc=$?
	if [[ $rc -ne 0 ]] && echo "$out" | grep -q "entry for v0.2.1"; then
		pass "stale changelog entry → error"
	else
		fail "stale changelog entry → error (rc=$rc, got: $out)"
	fi
	rm -rf "$dir" "$mock"
}

test_missing_release_notes() {
	local dir mock
	dir=$(make_repo)
	add_tag "$dir" "v0.3.0"
	add_commit "$dir" "feat: something great"

	printf '## v0.3.1 (2025-01-01)\n\n- new entry\n' >"$dir/CHANGELOG.md"
	mock=$(mock_tools)

	local rc=0 out
	out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || rc=$?
	if [[ $rc -ne 0 ]] && echo "$out" | grep -q ".release-notes.md"; then
		pass "missing .release-notes.md → error"
	else
		fail "missing .release-notes.md → error (rc=$rc, got: $out)"
	fi
	rm -rf "$dir" "$mock"
}

# ── plugin version bump tests ────────────────────────────────────────────────

add_plugin_manifests() {
	local dir=$1 version=${2:-0.1.0}
	mkdir -p "$dir/.claude-plugin"
	cat >"$dir/.claude-plugin/marketplace.json" <<EOF
{
  "name": "gcx-marketplace",
  "plugins": [
    {
      "name": "gcx",
      "source": "./claude-plugin",
      "version": "${version}"
    }
  ]
}
EOF
	mkdir -p "$dir/claude-plugin/.claude-plugin"
	cat >"$dir/claude-plugin/.claude-plugin/plugin.json" <<EOF
{
  "name": "gcx",
  "version": "${version}"
}
EOF
	git -C "$dir" add .
	git -C "$dir" commit -q -m "chore: add plugin manifests"
}

test_plugin_version_bumped() {
	local dir mock
	dir=$(make_repo)
	add_tag "$dir" "v0.5.0"
	add_plugin_manifests "$dir" "0.5.0"
	add_commit "$dir" "feat: new skill"
	write_release_files "$dir" "v0.5.1"
	mock=$(mock_tools)

	(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1) || true

	local marketplace_ver plugin_ver
	marketplace_ver=$(grep '"version"' "$dir/.claude-plugin/marketplace.json" | head -1 | sed 's/.*"\([0-9][^"]*\)".*/\1/')
	plugin_ver=$(grep '"version"' "$dir/claude-plugin/.claude-plugin/plugin.json" | head -1 | sed 's/.*"\([0-9][^"]*\)".*/\1/')

	if [[ "$marketplace_ver" == "0.5.1" && "$plugin_ver" == "0.5.1" ]]; then
		pass "plugin version bumped: 0.5.0 → 0.5.1 in both files"
	else
		fail "plugin version bumped: expected 0.5.1, got marketplace=${marketplace_ver} plugin=${plugin_ver}"
	fi
	rm -rf "$dir" "$mock"
}

test_plugin_version_no_files() {
	local dir mock
	dir=$(make_repo)
	add_tag "$dir" "v0.3.0"
	add_commit "$dir" "fix: something"
	write_release_files "$dir" "v0.3.1"
	mock=$(mock_tools)

	local out
	out=$(cd "$dir" && PATH="$mock:$PATH" DRY_RUN=1 bash "$SCRIPT" patch 2>&1)
	if echo "$out" | grep -q "v0.3.1"; then
		pass "no plugin files → tag script still succeeds"
	else
		fail "no plugin files → tag script still succeeds (got: $out)"
	fi
	rm -rf "$dir" "$mock"
}

# ── run all ───────────────────────────────────────────────────────────────────

echo "Running tag.sh tests..."
echo

test_no_bump_arg
test_invalid_bump
test_svu_not_installed
test_no_new_commits
test_bump_patch
test_bump_minor
test_bump_major
test_fallback_no_tags
test_missing_changelog_entry
test_stale_changelog_entry
test_missing_release_notes
test_plugin_version_bumped
test_plugin_version_no_files

echo
echo "Results: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
