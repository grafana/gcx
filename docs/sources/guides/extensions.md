---
title: Use and publish extensions
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Install, run, update and publish gcx extensions.
keywords:
  - gcx
  - extensions
  - plugins
weight: 40
---

# Use and publish `gcx` extensions

Extensions add commands to `gcx` without changing it. An extension is an executable that `gcx` starts when you run `gcx ext <name>`. It reads Grafana data by asking `gcx` to run commands, so it doesn't need your credentials.

{{< admonition type="warning" >}}
Extensions are third-party programs. They run with your permissions and can use your `gcx` credentials through the commands they run. Grafana Labs does not review or support them. Install only extensions you trust.
{{< /admonition >}}

## Install and run extensions

Install an extension from a git repository. `gcx` installs the latest release tag and verifies the downloaded executable against the checksum in the extension's manifest:

```shell
gcx ext install https://github.com/example/gcx-hello.git
```

`gcx` accepts `https://`, `ssh://` and `user@host:path` git URLs and requires `git`. Run the extension with its name. `gcx` flags go before the name; every argument after it is passed to the extension:

```shell
gcx ext hello --name world
gcx --context prod ext hello
```

Commands the extension runs use the same context, configuration file and agent mode as the `gcx ext` invocation, unless the extension overrides them.

Manage installed extensions with these commands:

```shell
gcx ext list
gcx ext update          # every extension; exits 4 if any fail
gcx ext update hello
gcx ext uninstall hello
```

`gcx` installs extensions under `$XDG_DATA_HOME/gcx/extensions`, which defaults to `~/.local/share/gcx/extensions`.

## Publish an extension

Build the extension with the [Go extension SDK](https://github.com/grafana/gcx/tree/main/extsdk). Extensions in other languages implement the SDK's gRPC protocol.

Add a `gcx-extension.yaml` manifest to the root of the repository:

```yaml
name: hello
version: 0.1.0
description: Say hello to a Grafana stack
minGCXVersion: 0.7.0
platforms:
  - os: darwin
    arch: arm64
    url: https://github.com/example/gcx-hello/releases/download/v0.1.0/hello_darwin_arm64.tar.gz
    sha256: <sha256 of the archive>
    bin: hello
  - os: linux
    arch: amd64
    url: https://github.com/example/gcx-hello/releases/download/v0.1.0/hello_linux_amd64.tar.gz
    sha256: <sha256 of the archive>
    bin: hello
  - os: "*"
    arch: "*"
    path: bin/hello
```

| Field | Description |
| :- | :- |
| `name` | The command name. Lowercase letters, digits and hyphens. |
| `version` | The extension's semantic version. |
| `description` | Shown by `gcx ext list`. |
| `minGCXVersion` | Optional. The oldest `gcx` version the extension supports. |
| `platforms` | One entry per OS and architecture, using Go's `GOOS` and `GOARCH` names. |
| `platforms[].url` | An `https://` URL of the executable, or of a `.tar.gz`, `.tgz` or `.zip` archive holding it. |
| `platforms[].sha256` | The hex SHA-256 checksum of the file at `url`. |
| `platforms[].bin` | The executable's path inside the archive. Required for archives. |
| `platforms[].path` | An executable relative to the manifest, used only by local installs. `os` and `arch` can be `*`. |

To release, upload the artifacts, update the manifest's `version`, URLs and checksums, commit it and push a semantic version tag such as `v0.1.0`. `gcx ext install` and `gcx ext update` use the highest tag that is not a pre-release.

## Develop an extension locally

Install from a local directory that holds the manifest. `gcx` runs the executable at the matching `path` entry in place, so rebuilding it needs no reinstall:

```shell
go build -o bin/hello .
gcx ext install .
gcx ext hello
```
