---
title: Installation
weight: -1
---

# Installation

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
