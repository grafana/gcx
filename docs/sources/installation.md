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

## Install on Windows

Use Windows 10 or later, or Windows Server 2016 or later, on AMD64 or ARM64.
Run this command in PowerShell 7 or Windows PowerShell 5.1:

```powershell
irm https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.ps1 | iex
gcx --version
```

The script detects AMD64 or ARM64. It downloads the matching Windows ZIP file
from the latest release. It verifies the SHA-256 checksum before installation.
It installs `gcx.exe` in `$env:LOCALAPPDATA\gcx\bin` by default. It adds that
directory to your user `PATH` and to the current PowerShell process.

You can run `gcx` from PowerShell or Command Prompt (`cmd.exe`). Simple command
hints work in both shells. Hints with quoted arguments use PowerShell syntax.
Use PowerShell to run those hints.

To upgrade, run the same command again. To install a specific version or use
another directory, set the environment variables before you run the command:

```powershell
$env:GCX_VERSION = '0.2.4'
$env:GCX_INSTALL_DIR = 'C:\Tools\gcx'
irm https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.ps1 | iex
```

`GCX_VERSION` accepts a release version. `GCX_INSTALL_DIR` sets the directory
for `gcx.exe`. The script also accepts `VERSION` and `INSTALL_DIR` for
compatibility. The `GCX_` names take precedence. Remove these environment
variables to return to the defaults.

To uninstall the default installation, remove the file and its `PATH` entry:

```powershell
$installDir = Join-Path $env:LOCALAPPDATA 'gcx\bin'
Remove-Item (Join-Path $installDir 'gcx.exe')
$key = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
try {
    $userPath = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $newPath = ($userPath -split ';' | Where-Object { $_ -and $_ -ne $installDir }) -join ';'
    $key.SetValue('Path', $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
} finally {
    $key.Dispose()
}
$env:Path = ($env:Path -split ';' | Where-Object { $_ -and $_ -ne $installDir }) -join ';'
```

If you set `GCX_INSTALL_DIR`, use that directory in the uninstall commands.
The commands leave other files in the directory in place. Sign out of Windows
and sign in again to refresh `PATH` in new applications.

### Install a Windows ZIP file manually

Download the `gcx_<version>_windows_amd64.zip` or
`gcx_<version>_windows_arm64.zip` file from the
[release page](https://github.com/grafana/gcx/releases/latest). Download the
matching `gcx_<version>_checksums.txt` file. Compare the ZIP file's SHA-256
value with the value in that file:

```powershell
Get-FileHash .\gcx_0.2.4_windows_amd64.zip -Algorithm SHA256
```

Replace the example version and architecture with the names of the files you
downloaded. Confirm that the hash matches the checksum file. Extract
`gcx.exe` from the ZIP file. Put it in a directory on
your `PATH`. Run `gcx --version` to check it.

These commands install the native Windows program. If you use WSL 2, run the
Linux install command inside WSL. The Linux installer installs a Linux binary.

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

## Install a prebuilt binary

Prebuilt binaries are available for a variety of systems and architectures. Refer to the [release versions on GitHub](https://github.com/grafana/gcx/releases/latest) for more details. For Windows ZIP files, use the [Windows instructions](#install-a-windows-zip-file-manually).

To install a prebuilt binary:

1. Download the archive for the operating system and architecture you need.
1. Extract the archive.
1. Move the executable to the directory where you want to keep it.
1. Make sure that directory is in your `PATH`.
1. On Linux or macOS, make sure the file has execute permission.

If you use macOS, a manually downloaded binary might be blocked by Gatekeeper.
For more information, refer to [macOS Gatekeeper and killed: 9](#macos-gatekeeper-and-killed-9).

## Run `gcx` in a container

Each release publishes a multi-arch (`linux/amd64`, `linux/arm64`) image to
`ghcr.io/grafana/gcx`. The entrypoint is `gcx`:

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

## Install `gcx` from source

To install `gcx` with Go, you need:

- [`git`](https://git-scm.com/).
- [`go`](https://go.dev/) 1.24 or later.

To install, run:

```shell
go install github.com/grafana/gcx/cmd/gcx@latest
```

## The version does not change after an upgrade

This page lists several install methods, and they write `gcx` to different
directories. If you use two methods, you get two copies. Your shell runs the
copy in the directory that comes first in `PATH`, and an upgrade of the other
copy changes nothing that you can see.

List every copy:

```sh
which -a gcx
```

On Windows, use `Get-Command gcx -All` in PowerShell. It lists each copy in
your `PATH`.

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

**Homebrew users are not affected**. Homebrew does not set the `xattr` on the files that it installs.

### Bypass the macOS gatekeeper

In manual downloads, bypass this by clearing the `xattr` and ad-hoc sign the binary so Apple Silicon accepts it:

```sh
xattr -d com.apple.quarantine "$(command -v gcx)" 2>/dev/null || true
codesign --sign - --force "$(command -v gcx)"   # required on Apple Silicon
```

Next, run `gcx --version` again; subsequent invocations should succeed without the block. 

Note that these steps will no longer be necessary once `gcx` release binaries are Apple-notarised.


