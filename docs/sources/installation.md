---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/installation/
title: Install gcx
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 2
---

# Install `gcx`

## Quick install using the script

The fastest way to install `gcx` on Linux or macOS is with the install script:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | sh
```

The script: 

- Detects your operating system and architecture.
- Downloads the latest release from GitHub 
- Verifies the SHA-256 checksum.
- Installs the binary to `~/.local/bin`.

The checksum check catches a corrupted download, but it does not prove the
archive came from the gcx release workflow. To check that, refer to
[Verify a release](#verify-a-release).

### Upgrade

To upgrade, run the same command again. The script always installs the latest release.

Check the result:

```sh
gcx --version
```

If the version doesn't change, refer to [The version does not change after an upgrade](#the-version-does-not-change-after-an-upgrade).

### Installer configuration options

Use these environment variables to customize the install script:

| Environment variable | Default | Description |
|----------------------|---------|-------------|
| `GCX_INSTALL_DIR` | `$HOME/.local/bin` | Directory to install the binary into |
| `GCX_VERSION` | latest | Specific version to install (e.g., `0.2.4`) |
| `GITHUB_TOKEN` | unset | GitHub token for API requests (avoids rate limits) |

The script also accepts `INSTALL_DIR` and `VERSION`. The `GCX_` names take
precedence. Prefer the `GCX_` names, because `INSTALL_DIR` and `VERSION` are
common names, and `curl | sh` inherits every variable that your shell exports.

### Examples

Install a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | GCX_VERSION=0.2.4 sh
```

Install to `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | GCX_INSTALL_DIR=/usr/local/bin sh
```

### Uninstall

To remove `gcx`, delete the binary:

```sh
rm ~/.local/bin/gcx
```

## Install `gcx` with Homebrew (macOS and Linux)

To install `gcx` with Homebrew run:

```shell
brew install gcx
```

This command installs the `gcx` formula from homebrew-core. Homebrew has a prebuilt bottle for macOS and Linux, so the install takes seconds. You do not need to add a tap.

To upgrade an existing installation:

```shell
brew upgrade gcx
```

### Install from the Grafana tap

The Grafana tap also carries `gcx`. Use the tap if you want Homebrew to compile the binary on your machine:

```shell
brew install grafana/grafana/gcx
```

Homebrew installs `go` as a build dependency for this formula. The first install usually takes 30 to 60 seconds, and later upgrades reuse the Homebrew download cache.

Install `gcx` from one source only. Two Homebrew formulas with the same name conflict with each other.

### Homebrew and macOS Gatekeeper

Both Homebrew methods avoid the macOS Gatekeeper problem. Homebrew does not set the quarantine attribute on the files that it installs, so you do not need to work around notarisation.

## Install `gcx` with asdf

The [asdf-gcx](https://github.com/grafana/asdf-gcx) plugin installs the prebuilt release binaries for macOS and Linux, on amd64 and arm64.

To install `gcx` with asdf:

```bash
asdf plugin add gcx https://github.com/grafana/asdf-gcx.git
asdf install gcx latest
asdf set -u gcx latest
gcx --version
```

On asdf releases before 0.16, use `asdf global gcx latest` instead of `asdf set -u gcx latest`.

## Install a prebuilt binary

Prebuilt binaries are available for a variety of systems and architectures. 

To install a prebuilt binary:

1. Download the archive for the operating system and architecture you need from the [releases page](https://github.com/grafana/gcx/releases/latest).
1. Extract the archive.
1. Move the executable to the directory where you want to keep it.
1. Make sure that directory is in your `PATH`.
1. Make sure the file has execute permission.

```bash
tar xzf gcx_*.tar.gz
chmod +x gcx && sudo mv gcx /usr/local/bin/
```

If you use macOS, a manually downloaded binary might be blocked by Gatekeeper. For more information, refer to [macOS Gatekeeper and killed: 9](#macos-gatekeeper-and-killed-9).

## Install `gcx` from source

To install `gcx` with Go, you need:

- [`git`](https://git-scm.com/).
- [`go`](https://go.dev/) 1.24 or later.

To install, run:

```shell
go install github.com/grafana/gcx/cmd/gcx@latest
```

## Shell completion

To run shell completion:

```bash
gcx completion zsh > "${fpath[1]}/_gcx"   # zsh
gcx completion bash > /etc/bash_completion.d/gcx  # bash
gcx completion fish > ~/.config/fish/completions/gcx.fish  # fish
```

## Run `gcx` in a container

Each release publishes the same multi-arch (`linux/amd64`, `linux/arm64`) image to two registries:

- `docker.io/grafana/gcx` (Docker Hub)
- `ghcr.io/grafana/gcx` (GitHub Container Registry)

Both carry identical tags and the same image digests. The entrypoint is `gcx`.

From Docker Hub:

```sh
docker run --rm docker.io/grafana/gcx:latest --version
```

From GitHub Container Registry:

```sh
docker run --rm ghcr.io/grafana/gcx:latest --version
```

Pass credentials through `GRAFANA_*` environment variables, or mount a config
file and point `GCX_CONFIG` at it, for example
`-v "$HOME/.config/gcx:/gcx:ro" -e GCX_CONFIG=/gcx/config.yaml`.
To pin a release in CI, use an exact tag such as `v1.5.0`. The `v1.5` and
`v1` tags follow the latest patch and minor release of that line. Prereleases
only get their exact tag.

The image is based on distroless and has no shell, so run `gcx` directly, not
through `sh -c`.

## Verify a release

Releases after v1.6.0 are signed keylessly with [Sigstore](https://www.sigstore.dev/).
Earlier releases have no signatures, and `cosign` reports `no matching signatures` for them.
There is no gcx public key to fetch: the release workflow signs with a
short-lived certificate issued against its GitHub Actions OIDC token, so a
signature proves an artifact came out of `.github/workflows/release.yaml` in
`grafana/gcx` on a `v*` tag.

Verification needs [`cosign`](https://docs.sigstore.dev/cosign/installation/)
v3 or later.

Two values appear in every command below:

- `--certificate-identity` — the signing workflow, including the tag it ran on.
- `--certificate-oidc-issuer` — always `https://token.actions.githubusercontent.com` for GitHub Actions.

Check both. A signature with a valid certificate but a different identity was
made by a different workflow.

### Verify the image

Replace `v1.5.0` with the tag you pulled:

```sh
cosign verify \
  --certificate-identity 'https://github.com/grafana/gcx/.github/workflows/release.yaml@refs/tags/v1.5.0' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  ghcr.io/grafana/gcx:v1.5.0
```

The floating tags (`latest`, `v1.5`, `v1`) move between releases, so you do not
know up front which tag signed the image behind them. Match any `v*` tag with
an anchored regular expression instead:

```sh
cosign verify \
  --certificate-identity-regexp '^https://github\.com/grafana/gcx/\.github/workflows/release\.yaml@refs/tags/v.*$' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  ghcr.io/grafana/gcx:latest
```

Keep the `^` and `$` anchors and the escaped dots. An unanchored pattern also
matches identities from other repositories and other workflows.

### Verify the archives

The release signs the checksums file rather than each archive, and that file
covers every archive in the release. Download the archive you want,
`gcx_<version>_checksums.txt`, and `gcx_<version>_checksums.txt.sigstore.json`
from the [release page](https://github.com/grafana/gcx/releases/latest) into
the same directory.

Verify the checksums file first:

```sh
cosign verify-blob \
  --certificate-identity 'https://github.com/grafana/gcx/.github/workflows/release.yaml@refs/tags/v1.5.0' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  --bundle gcx_1.5.0_checksums.txt.sigstore.json \
  gcx_1.5.0_checksums.txt
```

Only once that passes, check the archive against it:

```sh
sha256sum --check --ignore-missing gcx_1.5.0_checksums.txt
```

On macOS, use `shasum -a 256 --check --ignore-missing` instead.

Doing it in this order matters. `sha256sum` on its own only proves the archive
matches the checksums file next to it; verifying the signature first is what
ties that file to the release workflow.

### What the signature does and does not prove

A keyless signature proves which workflow built the artifact. It is therefore
only as strong as the controls on who can push `v*` tags and who can change
`.github/workflows/` in `grafana/gcx`. It says nothing about the contents of
the code itself.

## Troubleshooting

### The version does not change after an upgrade

This page lists several install methods, and they write `gcx` to different directories. If you use two methods, you get two copies. Your shell runs the copy in the directory that comes first in `PATH`, and an upgrade of the other copy changes nothing that you can see.

List every copy:

```sh
which -a gcx
```

The first line is the copy that your shell runs. Remove the copies that you don't want:

| Path | Install method | Command that removes it |
|------|----------------|-------------------------|
| `~/.local/bin/gcx` | Install script | `rm ~/.local/bin/gcx` |
| `/usr/local/bin/gcx` | Prebuilt binary, or the script with `GCX_INSTALL_DIR` | `sudo rm /usr/local/bin/gcx` |
| `/opt/homebrew/bin/gcx`, `/home/linuxbrew/.../gcx` | Homebrew | `brew uninstall gcx` |
| `~/go/bin/gcx` | `go install` | `rm ~/go/bin/gcx` |

After you remove a copy, your shell can still hold the old path in its command hash table. Open a new terminal, or run:

```sh
hash -r
```

The install script reports this problem for you. It names both paths and both versions, and it gives the removal command.

### macOS Gatekeeper and killed 9

macOS quarantines any downloaded binary by default. Since `gcx` release binaries are not yet Apple-notarised, macOS may block it the first time you run it. If this happens, you'll see one of these two symptoms:

- **Intel macOS**: A dialog says, *"Apple could not verify 'gcx' is free of malware…"*, and the binary doesn't run.
- **Apple Silicon (M-series) macOS**: The binary exits immediately with `killed: 9` and no visible dialog.

**Homebrew users are not affected**. Homebrew does not set the `xattr` on the files that it installs.

#### Bypass the macOS gatekeeper

In manual downloads, bypass this by clearing the `xattr` and ad-hoc sign the binary so Apple Silicon accepts it:

```sh
xattr -d com.apple.quarantine "$(command -v gcx)" 2>/dev/null || true
codesign --sign - --force "$(command -v gcx)"   # required on Apple Silicon
```

Next, run `gcx --version` again; subsequent invocations should succeed without the block. 

Note that these steps will no longer be necessary once `gcx` release binaries are Apple-notarised.