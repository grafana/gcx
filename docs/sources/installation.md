---
title: Installation
weight: -1
---

# Installation

## Quick install

The fastest way to install `gcx` on Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | sh
```

The script detects your OS and architecture, downloads the latest release from GitHub,
verifies the SHA-256 checksum, and installs the binary to `~/.local/bin`.

### Options

| Environment variable | Default | Description |
|----------------------|---------|-------------|
| `INSTALL_DIR` | `$HOME/.local/bin` | Directory to install the binary into |
| `VERSION` | latest | Specific version to install (e.g., `0.2.4`) |
| `GITHUB_TOKEN` | unset | GitHub token for API requests (avoids rate limits) |

### Examples

Install a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | VERSION=0.2.4 sh
```

Install to `/usr/local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | INSTALL_DIR=/usr/local/bin sh
```

### Uninstall

```sh
rm ~/.local/bin/gcx
```

## Homebrew (macOS and Linux)

```shell
brew install grafana/grafana/gcx
```

To upgrade:

```shell
brew upgrade grafana/grafana/gcx
```

The formula compiles gcx from source on your machine (Homebrew pulls `go`
as a build dependency). First install takes ~30–60 seconds; subsequent
upgrades reuse the Homebrew download cache. This path sidesteps macOS
Gatekeeper entirely — no notarisation workaround needed.

## Prebuilt binaries

Prebuilt binaries are available for a variety of operating systems and architectures.
Visit the [latest release](https://github.com/grafana/gcx/releases/latest) page, and scroll down to the Assets section.

* Download the archive for the desired operating system and architecture
* Extract the archive
* Move the executable to the desired directory
* Ensure this directory is included in the `PATH` environment variable
* Verify that you have execute permission on the file

On macOS, a manually-downloaded binary may be blocked by Gatekeeper — see
[macOS Gatekeeper and killed: 9](#macos-gatekeeper-and-killed-9) below.

## macOS Gatekeeper and killed: 9

gcx release binaries are not yet Apple-notarised. On macOS, the OS may block
manually-downloaded binaries the first time you run them, with one of two
symptoms:

- **Intel macOS**: a dialog reading *"Apple could not verify 'gcx' is free of
  malware…"* and the binary refuses to run.
- **Apple Silicon (M-series) macOS**: the binary exits immediately with
  `killed: 9` and no visible dialog.

Both symptoms come from the same underlying cause — the quarantine extended
attribute that macOS attaches to any downloaded binary. The `curl | sh`
installer above clears it automatically. **Homebrew users are not affected**
— the formula compiles gcx from source on your machine, so no pre-built
binary is downloaded and no xattr is set. For manual downloads, clear the
xattr and ad-hoc sign the binary so Apple Silicon accepts it:

```sh
xattr -d com.apple.quarantine "$(command -v gcx)" 2>/dev/null || true
codesign --sign - --force "$(command -v gcx)"   # required on Apple Silicon
```

Rerun `gcx --version` afterwards; subsequent invocations should succeed
without the block. These steps will no longer be necessary once gcx release
binaries are Apple-notarised (tracked in a separate signing/notarisation ADR).

## Build from source

To build `gcx` from source you must:

* have [`git`](https://git-scm.com/) installed
* have [`go`](https://go.dev/) v1.24 (or greater) installed

```shell
go install github.com/grafana/gcx/cmd/gcx@latest
```
