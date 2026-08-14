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

The fastest way to install `gcx` on Linux or macOS is with the script:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | sh
```

The script: 

- Detects your operating system and architecture.
- Downloads the latest release from GitHub 
- Verifies the SHA-256 checksum.
- Installs the binary to `~/.local/bin`.

### Upgrade

To upgrade, run the same command again. The script always installs the latest release.

Check the result:

```sh
gcx --version
```

If the version does not change, refer to [The version does not change after an upgrade](#the-version-does-not-change-after-an-upgrade).

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

To upgrade an existing installation:

```shell
brew upgrade gcx
```

Homebrew builds `gcx` from source on your machine, as it pulls `go` as a build dependency.
The first install usually takes 30 to 60 seconds, and later upgrades reuse the Homebrew download cache.

This option avoids macOS Gatekeeper because it doesn't download a prebuilt binary. You
won't need to work around notarisation.

## Install a prebuilt binary

Prebuilt binaries are available for a variety of systems and architectures. Refer to the [release versions on GitHub](https://github.com/grafana/gcx/releases/latest) for more details.

To install a prebuilt binary:

1. Download the archive for the operating system and architecture you need.
1. Extract the archive.
1. Move the executable to the directory where you want to keep it.
1. Make sure that directory is in your `PATH`.
1. Make sure the file has execute permission.

If you use macOS, a manually downloaded binary might be blocked by Gatekeeper.
For more information, refer to [macOS Gatekeeper and killed: 9](#macos-gatekeeper-and-killed-9).

## Install `gcx` from source

To install `gcx` with Go, you need:

- [`git`](https://git-scm.com/).
- [`go`](https://go.dev/) 1.24 or later.

To install, run:

```shell
go install github.com/grafana/gcx/cmd/gcx@latest
```

## The version does not change after an upgrade

This page lists five install methods. Each one writes `gcx` to a different
directory. If you use two methods, you get two copies. Your shell runs the copy
in the directory that comes first in `PATH`, and an upgrade of the other copy
changes nothing that you can see.

List every copy:

```sh
which -a gcx
```

The first line is the copy that your shell runs. Remove the copies that you do
not want:

| Path | Install method | Command that removes it |
|------|----------------|-------------------------|
| `~/.local/bin/gcx` | Install script | `rm ~/.local/bin/gcx` |
| `/usr/local/bin/gcx` | Prebuilt binary, or the script with `GCX_INSTALL_DIR` | `sudo rm /usr/local/bin/gcx` |
| `/opt/homebrew/bin/gcx`, `/home/linuxbrew/.../gcx` | Homebrew | `brew uninstall gcx` |
| `~/go/bin/gcx` | `go install` | `rm ~/go/bin/gcx` |

After you remove a copy, your shell can still hold the old path in its command
hash table. Open a new terminal, or run:

```sh
hash -r
```

The install script reports this problem for you. It names both paths and both
versions, and it gives the removal command.

## macOS Gatekeeper and killed 9

macOS quarantines any downloaded binary by default. Since `gcx` release binaries are not yet Apple-notarised, macOS may block it the first time you run it. If this happens, you'll see one of these two symptoms:

- **Intel macOS**: A dialog says, *"Apple could not verify 'gcx' is free of malware…"*, and the binary doesn't run.
- **Apple Silicon (M-series) macOS**: The binary exits immediately with `killed: 9` and no visible dialog.

**Homebrew users are not affected**. Since it compiles `gcx` from source on your machine, no pre-built binary is downloaded and no `xattr` is set.

### Bypass the macOS gatekeeper

In manual downloads, bypass this by clearing the `xattr` and ad-hoc sign the binary so Apple Silicon accepts it:

```sh
xattr -d com.apple.quarantine "$(command -v gcx)" 2>/dev/null || true
codesign --sign - --force "$(command -v gcx)"   # required on Apple Silicon
```

Next, run `gcx --version` again; subsequent invocations should succeed without the block. 

Note that these steps will no longer be necessary once `gcx` release binaries are Apple-notarised.


