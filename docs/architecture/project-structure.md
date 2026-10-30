# Project Structure and Build System

## Source Boundaries

| Area | Responsibility |
|---|---|
| `cmd/gcx/` | Executable entry point and top-level CLI composition |
| `internal/` | Implementation packages organized by domain; provider packages also contribute command factories |
| `experimental/` | Separate Go modules with their own build and compatibility boundaries |
| `claude-plugin/` | Distributable agent workflows and bundled skill lifecycle metadata |
| `.agents/skills/` | Repository contributor workflows |
| `scripts/` | Build and documentation generators |
| `docs/` | Architecture, design, contributor guidance, and published documentation sources |
| `testdata/` | Shared fixtures and local integration configuration |

The main module intentionally has no supported public Go API. Go's `internal/` visibility rule allows its implementation to evolve without external-library compatibility obligations. The separately versioned [sandbox module](../../experimental/sandbox/README.md) provides an embedding API.

Locate implementation by domain rather than maintaining a mirrored package tree here. Start at [CLI composition](../../cmd/gcx/root/command.go), [resource pipelines](../../internal/resources/), [provider registration](../../internal/providers/), or [query clients](../../internal/query/). [Architecture](architecture.md) explains how those responsibilities connect.

## Build and Dependencies

[mise.toml](../../mise.toml) is authoritative for toolchain pins and tasks; [CONTRIBUTING.md](../../CONTRIBUTING.md) describes development setup and required checks. Build the executable into `bin/gcx`. Version, revision, and build date are injected at link time; an untagged development build reports a snapshot version.

Dependencies use Go module mode. [go.mod](../../go.mod) and [go.sum](../../go.sum) are the canonical dependency and checksum records; the shared module cache replaces a committed vendor tree. Lint runs with read-only module resolution. Keep any separate experimental module's dependency and build requirements in that module's documentation.

## Host and Sandbox Builds

The sandbox compiles gcx for `GOOS=wasip1`. Host-only functionality has paired implementations behind build tags: the sandbox has no outbound sockets, uses host-provided HTTP transport, and omits terminal-dependent command areas. Changes to paired functions must cover both implementations.

The main repository's wasip1 vet task checks packages that build without third-party stubs. The sandbox's build script and tests exercise the full module with its dependency patches. A host build alone does not verify sandbox compatibility; see the [sandbox README](../../experimental/sandbox/README.md) for the build and runtime contract.

## CI and Release Boundaries

[GitHub Actions workflows](../../.github/workflows/) define the actual triggers and jobs. Pull-request checks cover lint, tests, and the Grafana website documentation build; sandbox changes also have their module-specific build and test checks. Release workflows publish the CLI and sandbox through their respective versioning paths. Use the repository's [release skill](../../.agents/skills/release/SKILL.md) for release steps.

The local integration environment is defined by [docker-compose.yml](../../docker-compose.yml) and its fixtures. It enables Grafana's Kubernetes-compatible API and supplies a ready-to-use configuration under `testdata/`. Keep image versions, ports, and credentials in those source files rather than copying them into architecture guidance.

## Documentation Generation

Published pages live under `docs/sources/`. The normal Docker-based documentation build validates those checked-in pages without regenerating the released CLI reference. Repository architecture and design guides remain separate from the published release snapshot.

The release reference renderer derives command help from Cobra and configuration/environment documentation from struct tags and GoDoc. Generator tests cover command coverage, anchors, flag inheritance, and stable output. The snapshot describes a release, so it must not be compared for equality with unreleased commands on `main`.

Reference refresh is an explicit release operation: the release workflow refreshes from the upcoming release checkout, while standalone refreshes use the latest stable release. See [documentation maintenance](../reference/doc-maintenance.md) for the current procedure. Linter configuration, generator implementation, and workflow files remain authoritative for their own details.
