#!/bin/sh
# install.sh — Download and install the latest gcx binary.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | sh
#
# Environment variables:
#   INSTALL_DIR    Directory to install into (default: $HOME/.local/bin)
#   VERSION        Specific version to install, without v prefix (default: latest)
#   GITHUB_TOKEN   GitHub token for API requests (avoids rate limits)

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

get_latest_version() {
    url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    auth_header=""
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        auth_header="Authorization: Bearer ${GITHUB_TOKEN}"
    fi

    if [ -n "$auth_header" ]; then
        response=$(curl -fsSL -H "$auth_header" "$url") || err "Failed to fetch latest release from GitHub API."
    else
        response=$(curl -fsSL "$url") || err "Failed to fetch latest release from GitHub API. If rate-limited, set GITHUB_TOKEN or VERSION."
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

main() {
    need_cmd curl
    need_cmd tar

    os=$(detect_os)
    arch=$(detect_arch)

    if [ -n "${VERSION:-}" ]; then
        version="${VERSION#v}"
    else
        info "Fetching latest release..."
        version=$(get_latest_version)
    fi

    install_dir="${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"
    archive="${BINARY_NAME}_${version}_${os}_${arch}.tar.gz"
    base_url="https://github.com/${GITHUB_REPO}/releases/download/v${version}"

    info "Installing ${BINARY_NAME} ${version} (${os}/${arch})"

    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT

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
        installed_version=$("${install_dir}/${BINARY_NAME}" --version 2>&1 | head -1)
        info "Installed: ${installed_version}"
    else
        info "Installed ${BINARY_NAME} to ${install_dir}/${BINARY_NAME}"
    fi

    # Check if install dir is in PATH.
    case ":${PATH}:" in
        *":${install_dir}:"*) ;;
        *)
            echo ""
            info "${install_dir} is not in your PATH. Add it by running:"
            echo ""
            info "  export PATH=\"${install_dir}:\$PATH\""
            echo ""
            info "Add that line to your shell profile (~/.bashrc, ~/.zshrc, etc.)"
            ;;
    esac

    echo ""
    info "To uninstall: rm ${install_dir}/${BINARY_NAME}"
}

main "$@"
