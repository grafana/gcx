#!/bin/sh
# install.sh — Download and install the latest gcx binary.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | sh
#
# Environment variables:
#   GCX_INSTALL_DIR  Directory to install into (default: $HOME/.local/bin)
#   GCX_VERSION      Version to install, or "main" to build the main branch
#                    with Go (default: latest release)
#   GITHUB_TOKEN     GitHub token for API requests (avoids rate limits)
#
# INSTALL_DIR and VERSION still work. The GCX_ names take precedence, because
# INSTALL_DIR and VERSION are common names that a caller can already export.

set -eu

GITHUB_REPO="grafana/gcx"
BINARY_NAME="gcx"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

info() {
    printf '  %s\n' "$@"
}

warn() {
    printf '  WARNING: %s\n' "$@" >&2
}

err() {
    printf '  ERROR: %s\n' "$@" >&2
    exit 1
}

need_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        err "Required command '$1' not found. Please install it and try again."
    fi
}

detect_os() {
    os="$(uname -s)"
    case "$os" in
        Linux)  echo "linux" ;;
        Darwin) echo "darwin" ;;
        *)      err "Unsupported OS: $os. This installer supports Linux and macOS." ;;
    esac
}

detect_arch() {
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              err "Unsupported architecture: $arch" ;;
    esac
}

detect_user_shell() {
    if [ -n "${SHELL:-}" ]; then
        printf '%s\n' "${SHELL##*/}"
    else
        printf '%s\n' "sh"
    fi
}

print_path_instructions() {
    install_dir="$1"
    shell_name=$(detect_user_shell)

    echo ""
    case "$shell_name" in
        bash)
            info "${install_dir} is not in your PATH. Add it with:"
            echo ""
            info "  echo 'export PATH=\"${install_dir}:\$PATH\"' >> ~/.bashrc"
            info "  . ~/.bashrc"
            ;;
        zsh)
            info "${install_dir} is not in your PATH. Add it with:"
            echo ""
            info "  echo 'export PATH=\"${install_dir}:\$PATH\"' >> ~/.zshrc"
            info "  source ~/.zshrc"
            ;;
        fish)
            info "${install_dir} is not in your PATH. Add it with:"
            echo ""
            info "  mkdir -p ~/.config/fish"
            info "  echo 'fish_add_path ${install_dir}' >> ~/.config/fish/config.fish"
            info "  source ~/.config/fish/config.fish"
            ;;
        *)
            info "${install_dir} is not in your PATH. Add it to your shell startup file:"
            echo ""
            info "  export PATH=\"${install_dir}:\$PATH\""
            ;;
    esac
}

# Print the path of the first executable named $1 on PATH.
# Return 1 when PATH holds no such executable.
#
# This walks PATH instead of calling `command -v`. The script looks the binary
# up before the copy and after it, and a shell can serve the second lookup from
# its command hash table. A stale hash is the exact failure this check reports,
# so the check must not depend on one.
resolve_on_path() {
    _name="$1"
    _saved_ifs="$IFS"
    set -f # A PATH entry must not expand as a glob.
    IFS=:
    for _dir in $PATH; do
        [ -n "$_dir" ] || _dir="."
        if [ -f "${_dir}/${_name}" ] && [ -x "${_dir}/${_name}" ]; then
            IFS="$_saved_ifs"
            set +f
            printf '%s\n' "${_dir}/${_name}"
            return 0
        fi
    done
    IFS="$_saved_ifs"
    set +f
    return 1
}

# Print the first line that "$1 --version" writes.
# Print "unknown version" when the binary does not answer.
binary_version() {
    _out=$("$1" --version 2>/dev/null | head -1) || _out=""
    if [ -n "$_out" ]; then
        printf '%s\n' "$_out"
    else
        printf '%s\n' "unknown version"
    fi
}

# Print the command that removes the binary at $1.
removal_command() {
    case "$1" in
        /opt/homebrew/* | /usr/local/Cellar/* | /home/linuxbrew/*)
            printf '%s\n' "brew uninstall ${BINARY_NAME}"
            ;;
        *)
            printf '%s\n' "rm ${1}"
            ;;
    esac
}

# Report that the shell runs a different binary than the one just installed.
warn_shadowed() {
    _target="$1"
    _other="$2"

    echo ""
    warn "Your shell runs a different ${BINARY_NAME}."
    echo ""
    info "Installed now:  ${_target} ($(binary_version "$_target"))"
    info "Shell runs:     ${_other} ($(binary_version "$_other"))"
    echo ""
    info "The other copy comes earlier in your PATH. Remove it with:"
    echo ""
    info "  $(removal_command "$_other")"
    echo ""
    info "Then run 'hash -r', or open a new terminal."
}

# Report which binary the shell will run after the install.
# $1 is the install directory. $2 is the binary that PATH resolved to before
# the install, or an empty string.
report_resolution() {
    _install_dir="$1"
    _previous_path="$2"
    _target="${_install_dir}/${BINARY_NAME}"

    _resolved=$(resolve_on_path "$BINARY_NAME") || _resolved=""

    case ":${PATH}:" in
        *":${_install_dir}:"*) _dir_on_path=1 ;;
        *) _dir_on_path=0 ;;
    esac

    if [ "$_dir_on_path" -eq 0 ]; then
        print_path_instructions "$_install_dir"
    fi

    if [ -n "$_resolved" ] && [ "$_resolved" != "$_target" ]; then
        warn_shadowed "$_target" "$_resolved"
    elif [ -n "$_previous_path" ] && [ "$_previous_path" != "$_target" ]; then
        echo ""
        info "Run 'hash -r', or open a new terminal, to pick up the new path."
    fi
}

get_latest_version() {
    url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    auth_header=""
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        auth_header="Authorization: Bearer ${GITHUB_TOKEN}"
    fi

    if [ -n "$auth_header" ]; then
        response=$(curl -fsSL -H "$auth_header" "$url") || err "Failed to fetch latest release from GitHub API."
    else
        response=$(curl -fsSL "$url") || err "Failed to fetch latest release from GitHub API. If rate-limited, set GITHUB_TOKEN or GCX_VERSION."
    fi

    tag=$(printf '%s' "$response" | grep '"tag_name"' | sed 's/.*"tag_name": *"//;s/".*//')
    if [ -z "$tag" ]; then
        err "Could not determine latest release tag."
    fi

    # Strip v prefix — archive filenames use bare version numbers.
    printf '%s' "${tag#v}"
}

verify_checksum() {
    archive_path="$1"
    expected="$2"

    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$archive_path" | cut -d' ' -f1)
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$archive_path" | cut -d' ' -f1)
    else
        warn "Neither sha256sum nor shasum found. Skipping checksum verification."
        return 0
    fi

    if [ "$actual" != "$expected" ]; then
        err "Checksum mismatch! Expected: ${expected}, got: ${actual}"
    fi
}

install_main() {
    output_dir="$1"

    need_cmd go
    mkdir -p "$output_dir"
    info "Building ${BINARY_NAME} from the main branch..."
    GOBIN="$output_dir" go install "github.com/${GITHUB_REPO}/cmd/${BINARY_NAME}@main" ||
        err "Failed to build ${BINARY_NAME} from the main branch. Check network access and your Go toolchain."

    if [ ! -x "${output_dir}/${BINARY_NAME}" ]; then
        err "The main branch build did not produce ${output_dir}/${BINARY_NAME}."
    fi
}

main() {
    os=$(detect_os)
    arch=$(detect_arch)

    # Record the binary that PATH resolves to now, before the copy replaces it.
    previous_path=$(resolve_on_path "$BINARY_NAME") || previous_path=""
    previous_version=""
    if [ -n "$previous_path" ]; then
        previous_version=$(binary_version "$previous_path")
    fi

    requested_version="${GCX_VERSION:-${VERSION:-}}"
    if [ -n "$requested_version" ]; then
        version="${requested_version#v}"
    else
        need_cmd curl
        info "Fetching latest release..."
        version=$(get_latest_version)
    fi

    install_dir="${GCX_INSTALL_DIR:-${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}}"
    archive="${BINARY_NAME}_${version}_${os}_${arch}.tar.gz"
    base_url="https://github.com/${GITHUB_REPO}/releases/download/v${version}"

    info "Installing ${BINARY_NAME} ${version} (${os}/${arch})"

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

    if [ "$version" = "main" ]; then
        install_main "$tmpdir"
    else
        need_cmd curl
        need_cmd tar

        # Download archive and checksums.
        info "Downloading ${archive}..."
        curl -fsSL "${base_url}/${archive}" -o "${tmpdir}/${archive}" ||
            err "Failed to download ${base_url}/${archive}"

        checksums_file="${BINARY_NAME}_${version}_checksums.txt"
        curl -fsSL "${base_url}/${checksums_file}" -o "${tmpdir}/${checksums_file}" ||
            err "Failed to download checksums file."

        # Verify checksum.
        expected=$(grep "${archive}" "${tmpdir}/${checksums_file}" | cut -d' ' -f1)
        if [ -z "$expected" ]; then
            err "Archive ${archive} not found in checksums file."
        fi
        verify_checksum "${tmpdir}/${archive}" "$expected"
        info "Checksum verified."

        # Extract binary.
        tar xzf "${tmpdir}/${archive}" -C "${tmpdir}" "${BINARY_NAME}" ||
            err "Failed to extract ${BINARY_NAME} from archive."
    fi

    # Install binary.
    mkdir -p "$install_dir"
    mv "${tmpdir}/${BINARY_NAME}" "${install_dir}/${BINARY_NAME}"
    chmod +x "${install_dir}/${BINARY_NAME}"

    # Remove macOS quarantine attribute if present.
    if [ "$os" = "darwin" ] && command -v xattr >/dev/null 2>&1; then
        xattr -d com.apple.quarantine "${install_dir}/${BINARY_NAME}" 2>/dev/null || true
    fi

    # Verify installation.
    if "${install_dir}/${BINARY_NAME}" --version >/dev/null 2>&1; then
        installed_version=$(binary_version "${install_dir}/${BINARY_NAME}")
        if [ -n "$previous_version" ] && [ "$previous_version" != "$installed_version" ]; then
            info "Installed: ${installed_version} (replaces: ${previous_version})"
        else
            info "Installed: ${installed_version}"
        fi
    else
        info "Installed ${BINARY_NAME} to ${install_dir}/${BINARY_NAME}"
    fi

    # Report which binary the shell runs. The copy above can land behind an
    # older binary that PATH resolves first, and the user then sees no change.
    report_resolution "$install_dir" "$previous_path"

    echo ""
    info "To uninstall: rm ${install_dir}/${BINARY_NAME}"
}

# The test file sources this script to call the functions on their own.
if [ "${GCX_INSTALL_SH_LIB:-}" != "1" ]; then
    main "$@"
fi
