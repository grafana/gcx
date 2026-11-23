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

# `gcx` extensions


`gcx` extensions are git repositories that provide additional gcx commands. `gcx` is used to fetch data from your Grafana instance, so the extension does not need to handle credentials or config. All arguments passed to `gcx ext <name>` will be forwarded to the extension.

{{< admonition type="warning" >}}
Extensions are third-party programs. They run with your permissions and can use your `gcx` credentials through the commands they run. Grafana Labs does not review or support them. Only install extensions that you have reviewed and trust.
{{< /admonition >}}

## Install and run extensions

Install an extension from any git repository. `gcx` installs the latest release tag and verifies the downloaded executable against the checksum in the extension's manifest:

```shell
gcx ext install https://github.com/example/gcx-hello.git
```

`gcx ext` requires git to install extensions. git URLs starting with `https://`, `ssh://` and `user@host:path` are accepted. Flags for `gcx` go before the extension name, and every argument after it is passed to the extension:

```shell
gcx ext hello --name world
gcx --context prod ext hello
```

By default, `gcx` commands run by the extension use the same context used when the extension is called.

Manage installed extensions with these commands:

```shell
gcx ext list
gcx ext update
gcx ext update hello
gcx ext uninstall hello
```

`gcx` installs extensions under `$XDG_DATA_HOME/gcx/extensions`, which defaults to `~/.local/share/gcx/extensions`.

## Build an extension

Build the extension with the [Go extension SDK](https://github.com/grafana/gcx/tree/main/extsdk). Extensions in other languages implement the SDK's gRPC protocol.

While you develop it, install the extension from its local directory. `gcx` runs the executable at the manifest's `path` entry in place, so rebuilding it needs no reinstall:

```shell
go build -o bin/hello .
gcx ext install .
gcx ext hello
```

## Publish an extension

A published extension is a git repository with a `gcx-extension.yaml` manifest at its root and a downloadable build for each OS and architecture it supports. `gcx ext install` and `gcx ext update` use the latest tag, and read the manifest at that tag. The manifest tells gcx where the relevant binaries are available.

### The extension manifest

Below is an example manifest, along with a descripition of what each property is for. There is guidance further down for compiling binaries for a Go program.

```yaml
name: hello
version: 0.1.0
description: Say hello to a Grafana stack
minGCXVersion: 0.7.0
platforms:
  - os: darwin
    arch: arm64
    url: https://github.com/example/gcx-hello/releases/download/v0.1.0/hello_darwin_arm64.tar.gz
    sha256: "<sha256 of the archive>"
    bin: hello
  - os: linux
    arch: amd64
    url: https://github.com/example/gcx-hello/releases/download/v0.1.0/hello_linux_amd64.tar.gz
    sha256: "<sha256 of the archive>"
    bin: hello
  - os: "*"
    arch: "*"
    path: bin/hello
```

| Field | Description |
| :- | :- |
| `name` | The command name, made up of lowercase letters, digits and hyphens. |
| `version` | The extension version, using semVer. |
| `description` | A short description of the extension, shown by `gcx ext list`. |
| `minGCXVersion` | Optional. The oldest `gcx` version supported by the extension. |
| `platforms` | One entry per OS and architecture, using Go's `GOOS` and `GOARCH` names. |
| `platforms[].url` | An `https://` URL of the executable, or of a `.tar.gz`, `.tgz` or `.zip` archive holding it. |
| `platforms[].sha256` | The hex SHA-256 checksum of the file at `url`. Quote it: YAML reads a checksum made only of digits as a number. |
| `platforms[].bin` | The executable's path inside the archive. Required for archives. |
| `platforms[].path` | An executable relative to the manifest, used only by local installs. `os` and `arch` can be `*`. |

### Release a version

The extension manifest at the release tag holds the checksums of the binaries, so you need to build and upload the binaries before addign a tag. [GoReleaser](https://goreleaser.com) is useful for building for each platform at once - we use this to build `gcx`. `gcx`'s own [`.goreleaser.yaml`](https://github.com/grafana/gcx/blob/main/.goreleaser.yaml) is a good reference, if you ignore the Docker and signing sections.

You can create a release for your extension with these steps:

1. (Once:) Add a `.goreleaser.yaml` to the extension's repository:

   ```yaml
   version: 2
   project_name: hello
   builds:
     - binary: hello
       env: [CGO_ENABLED=0]
       goos: [linux, darwin, windows]
       goarch: [amd64, arm64]
   archives:
     - formats: [tar.gz]
       name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"
       format_overrides:
         - goos: windows
           formats: [zip]
   checksum:
     name_template: checksums.txt
   ```

1. Build the archives into `dist/` without publishing:

   ```shell
   goreleaser release --snapshot --clean
   ```

1. Add a `platforms` row to the manifest for each archive in `dist/checksums.txt`, with its release download URL and checksum. Set `bin` to `hello`, or `hello.exe` for Windows.

1. Commit the manifest, then tag and upload the archives you just built:

   ```shell
   git commit -am "release v0.1.0"
   git tag v0.1.0
   git push origin main v0.1.0
   gh release create v0.1.0 dist/*.tar.gz dist/*.zip
   ```

Don't rebuild between steps 2 and 4: Go embeds the commit in the binary, and rebuilding the binaries changes the checksums.
