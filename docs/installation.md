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

## Homebrew (macOS/Linux)

```shell
brew install grafana/grafana/gcx
```

To upgrade:

```shell
brew upgrade gcx
```

## Prebuilt binaries

Prebuilt binaries are available for a variety of operating systems and architectures.
Visit the [latest release](https://github.com/grafana/gcx/releases/latest) page, and scroll down to the Assets section.

* Download the archive for the desired operating system and architecture
* Extract the archive
* Move the executable to the desired directory
* Ensure this directory is included in the `PATH` environment variable
* Verify that you have execute permission on the file

## Build from source

To build `gcx` from source you must:

* have [`git`](https://git-scm.com/) installed
* have [`go`](https://go.dev/) v1.24 (or greater) installed

```shell
go install github.com/grafana/gcx/cmd/gcx@latest
```
