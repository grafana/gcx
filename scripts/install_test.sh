#!/usr/bin/env bash
# Tests for scripts/install.sh
# Run with: bash scripts/install_test.sh

set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/install.sh"
PASS=0
FAIL=0

# The tests replace PATH. The script still needs the standard utilities, so
# every fake PATH ends with these directories. Neither one holds a gcx.
SYS_PATH="/usr/bin:/bin"

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

assert_eq() {
	local want=$1 got=$2 name=$3
	if [[ "$got" == "$want" ]]; then
		pass "$name"
	else
		fail "$name (want: '$want', got: '$got')"
	fi
}

assert_contains() {
	local haystack=$1 needle=$2 name=$3
	if [[ "$haystack" == *"$needle"* ]]; then
		pass "$name"
	else
		fail "$name (missing: '$needle')"
	fi
}

assert_not_contains() {
	local haystack=$1 needle=$2 name=$3
	if [[ "$haystack" != *"$needle"* ]]; then
		pass "$name"
	else
		fail "$name (unwanted: '$needle')"
	fi
}

# Write an executable that answers --version with the given version.
make_fake_gcx() {
	local dir=$1 version=$2
	mkdir -p "$dir"
	printf '#!/bin/sh\necho "gcx version %s"\n' "$version" >"$dir/gcx"
	chmod +x "$dir/gcx"
}

# Write a fake go command that records its arguments and produces the binary
# where go install would place it.
make_fake_go() {
	local dir=$1
	mkdir -p "$dir"
	cat >"$dir/go" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >"$GO_ARGS_LOG"
printf '#!/bin/sh\necho "gcx version main"\n' >"$GOBIN/gcx"
chmod +x "$GOBIN/gcx"
EOF
	chmod +x "$dir/go"
}

# Source the script. GCX_INSTALL_SH_LIB stops it from running main.
export GCX_INSTALL_SH_LIB=1
# shellcheck source=install.sh
. "$SCRIPT"

# ── resolve_on_path ──────────────────────────────────────────────────────────

test_resolve_returns_first_match() {
	local tmp
	tmp=$(mktemp -d)
	make_fake_gcx "$tmp/first" "1.0.0"
	make_fake_gcx "$tmp/second" "0.4.2"

	local got
	got=$(
		PATH="$tmp/first:$tmp/second:$SYS_PATH"
		resolve_on_path gcx
	) || got=""

	assert_eq "$tmp/first/gcx" "$got" "resolve_on_path returns the first match"
	rm -rf "$tmp"
}

test_resolve_skips_non_executable() {
	local tmp
	tmp=$(mktemp -d)
	mkdir -p "$tmp/first"
	printf '#!/bin/sh\n' >"$tmp/first/gcx"
	make_fake_gcx "$tmp/second" "0.4.2"

	local got
	got=$(
		PATH="$tmp/first:$tmp/second:$SYS_PATH"
		resolve_on_path gcx
	) || got=""

	assert_eq "$tmp/second/gcx" "$got" "resolve_on_path skips a file that is not executable"
	rm -rf "$tmp"
}

test_resolve_reports_absence() {
	local tmp rc=0
	tmp=$(mktemp -d)
	mkdir -p "$tmp/empty"

	(
		PATH="$tmp/empty:$SYS_PATH"
		resolve_on_path gcx >/dev/null
	) || rc=$?

	assert_eq "1" "$rc" "resolve_on_path returns 1 when PATH holds no match"
	rm -rf "$tmp"
}

# ── removal_command ──────────────────────────────────────────────────────────

test_removal_command_uses_brew() {
	assert_eq "brew uninstall gcx" "$(removal_command /opt/homebrew/bin/gcx)" \
		"removal_command names brew for a Homebrew path"
}

test_removal_command_uses_rm() {
	assert_eq "rm /usr/local/bin/gcx" "$(removal_command /usr/local/bin/gcx)" \
		"removal_command names rm for a plain path"
}

# ── main branch install ───────────────────────────────────────────────────────────────────────────

test_install_main_builds_main_ref() {
	local tmp args
	tmp=$(mktemp -d)
	make_fake_go "$tmp/bin"

	GO_ARGS_LOG="$tmp/go-args" PATH="$tmp/bin:$SYS_PATH" install_main "$tmp/output"
	args=$(cat "$tmp/go-args")

	assert_eq "install github.com/grafana/gcx/cmd/gcx@main" "$args" \
		"install_main builds the gcx main branch"
	if [[ -x "$tmp/output/gcx" ]]; then
		pass "install_main produces an executable gcx binary"
	else
		fail "install_main produces an executable gcx binary"
	fi
	rm -rf "$tmp"
}

test_main_dispatches_main_to_source_build() {
	local tmp out args
	tmp=$(mktemp -d)
	make_fake_go "$tmp/bin"

	out=$(
		GCX_VERSION=main \
			GCX_INSTALL_DIR="$tmp/install" \
			GO_ARGS_LOG="$tmp/go-args" \
			PATH="$tmp/bin:$SYS_PATH" \
			main
	)
	args=$(cat "$tmp/go-args")

	assert_eq "install github.com/grafana/gcx/cmd/gcx@main" "$args" \
		"GCX_VERSION=main selects the source build"
	assert_contains "$out" "Installed: gcx version main" \
		"the main branch build is installed and verified"
	if [[ -x "$tmp/install/gcx" ]]; then
		pass "GCX_VERSION=main installs gcx in GCX_INSTALL_DIR"
	else
		fail "GCX_VERSION=main installs gcx in GCX_INSTALL_DIR"
	fi
	rm -rf "$tmp"
}

# ── report_resolution ────────────────────────────────────────────────────────

test_report_warns_on_shadow() {
	local tmp out
	tmp=$(mktemp -d)
	make_fake_gcx "$tmp/old" "0.4.2"
	make_fake_gcx "$tmp/new" "1.0.0"

	out=$(
		PATH="$tmp/old:$tmp/new:$SYS_PATH"
		report_resolution "$tmp/new" "$tmp/old/gcx" 2>&1
	)

	assert_contains "$out" "Your shell runs a different gcx" \
		"report_resolution warns when another copy comes first"
	# Anchor the label prefix. Without it, a swap of the two paths inside
	# warn_shadowed keeps every substring present, and the test stays green.
	assert_contains "$out" "Installed now:  $tmp/new/gcx (gcx version 1.0.0)" \
		"the warning names the installed path and version"
	assert_contains "$out" "Shell runs:     $tmp/old/gcx (gcx version 0.4.2)" \
		"the warning names the shadowing path and version"
	assert_contains "$out" "rm $tmp/old/gcx" \
		"the warning gives the removal command"
	rm -rf "$tmp"
}

test_report_is_quiet_when_install_dir_wins() {
	local tmp out
	tmp=$(mktemp -d)
	make_fake_gcx "$tmp/old" "0.4.2"
	make_fake_gcx "$tmp/new" "1.0.0"

	out=$(
		PATH="$tmp/new:$tmp/old:$SYS_PATH"
		report_resolution "$tmp/new" "$tmp/old/gcx" 2>&1
	)

	assert_not_contains "$out" "Your shell runs a different gcx" \
		"report_resolution stays quiet when the install directory comes first"
	assert_contains "$out" "hash -r" \
		"report_resolution asks for a rehash when the resolved path changed"
	rm -rf "$tmp"
}

test_report_is_quiet_on_a_plain_reinstall() {
	local tmp out
	tmp=$(mktemp -d)
	make_fake_gcx "$tmp/new" "1.0.0"

	out=$(
		PATH="$tmp/new:$SYS_PATH"
		report_resolution "$tmp/new" "$tmp/new/gcx" 2>&1
	)

	assert_eq "" "$out" "report_resolution prints nothing when the path did not change"
	rm -rf "$tmp"
}

test_report_explains_a_missing_path_entry() {
	local tmp out
	tmp=$(mktemp -d)
	mkdir -p "$tmp/other"
	make_fake_gcx "$tmp/new" "1.0.0"

	out=$(
		PATH="$tmp/other:$SYS_PATH"
		report_resolution "$tmp/new" "" 2>&1
	)

	assert_contains "$out" "is not in your PATH" \
		"report_resolution explains a missing PATH entry"
	assert_not_contains "$out" "Your shell runs a different gcx" \
		"report_resolution reports no shadow when no other copy exists"
	rm -rf "$tmp"
}

test_report_covers_both_problems() {
	local tmp out
	tmp=$(mktemp -d)
	make_fake_gcx "$tmp/old" "0.4.2"
	make_fake_gcx "$tmp/new" "1.0.0"

	out=$(
		PATH="$tmp/old:$SYS_PATH"
		report_resolution "$tmp/new" "$tmp/old/gcx" 2>&1
	)

	assert_contains "$out" "is not in your PATH" \
		"report_resolution reports the missing PATH entry and the shadow together"
	assert_contains "$out" "Your shell runs a different gcx" \
		"report_resolution warns about the shadow when the directory is off PATH"
	rm -rf "$tmp"
}

# ── run ──────────────────────────────────────────────────────────────────────

test_resolve_returns_first_match
test_resolve_skips_non_executable
test_resolve_reports_absence
test_removal_command_uses_brew
test_removal_command_uses_rm
test_install_main_builds_main_ref
test_main_dispatches_main_to_source_build
test_report_warns_on_shadow
test_report_is_quiet_when_install_dir_wins
test_report_is_quiet_on_a_plain_reinstall
test_report_explains_a_missing_path_entry
test_report_covers_both_problems

echo
echo "Results: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
