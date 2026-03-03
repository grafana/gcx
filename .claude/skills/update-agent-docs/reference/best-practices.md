# Best Practices for Agent-Docs

> Rules and checks for maintaining `agent-docs/*.md` quality.
> Used by the `/update-agent-docs` skill during audits.

## Design Principles

Agent-docs are **high-level architecture references** for autonomous coding agents, originally generated via `/learn-codebase`. They deliberately avoid low-level details that change rapidly.

- **High-level**: Focus on patterns, design decisions, and architecture -- not exact line numbers or implementation details
- **Navigable**: Lightweight index (`README.md`) -> domain docs. Agents read the map first, then navigate on demand
- **Cross-linked**: `CLAUDE.md` (AGENTS.md) is the entry point; agent-docs are discoverable from it
- **Stable**: Docs should not need updating for every code change -- only for architectural shifts

## When Docs Need Updating

The skill checks for **structural changes** that shift architecture, not line-level edits:

| Trigger | Example | Affected Doc |
|---------|---------|-------------|
| New top-level package in `internal/` | `internal/providers/`, `internal/agent/` | `architecture.md`, `project-structure.md` |
| New command group in `cmd/grafanactl/` | `cmd/grafanactl/slo/`, `cmd/grafanactl/dev/` | `cli-layer.md` |
| New data flow pipeline | Provider-specific push path | `data-flows.md` |
| New config model structure | `Context.Providers` map | `config-system.md` |
| New architectural pattern | Provider interface, agent mode detection | `patterns.md` |
| Changed API surface | New client path beyond dynamic + OpenAPI | `client-api-layer.md` |
| New resource abstraction | Provider-specific Resource adapter | `resource-model.md` |
| New build/CI target | Makefile target, GH Actions workflow | `project-structure.md` |

## What the Skill Does NOT Check

- Exact line numbers or function signatures (these change frequently; docs are deliberately vague about them)
- Test coverage or CI configuration details
- Individual field additions to existing structs (unless they represent a new pattern)
- Formatting/style consistency (the docs were generated with consistent style by `/learn-codebase`)
- Code quality, linting, or correctness of implementations
- Documentation for planned/in-progress features (only check what exists in code)

## Structural Checks

### 1. Package Inventory

**Command**: `ls internal/`

**Rule**: Every top-level directory in `internal/` should appear in:
- `architecture.md` (in the layered architecture description or component list)
- `project-structure.md` (in the directory layout section)

**Severity**: Missing coverage (medium) if the package represents a new architectural layer. Low if it's a utility package nested under an existing layer.

### 2. Command Inventory

**Command**: `ls cmd/grafanactl/*/`

**Rule**: Every command group directory in `cmd/grafanactl/` should appear in:
- `cli-layer.md` (in the command tree section)
- The package map in `CLAUDE.md` (AGENTS.md)

**Severity**: Missing coverage (high) for user-facing command groups. Low for internal wiring directories.

### 3. Pattern Count

**Rule**: Count the patterns documented in `patterns.md`. Cross-reference against code to detect new patterns:
- Provider interface pattern (if `internal/providers/provider.go` exists)
- Agent mode detection (if `internal/agent/` exists or env detection in root command)
- Translation adapter pattern (if `adapter.go` files exist in provider packages)
- Prepare/Unprepare pattern for server-generated fields
- Any new interface with 3+ implementations

**Severity**: Missing coverage (medium) for patterns used across multiple packages. Low for single-use patterns.

### 4. Config Model

**Command**: Read `internal/config/types.go` (or equivalent)

**Rule**: The `GrafanaConfig` and `Context` struct shapes should match the data model described in `config-system.md`. Check for:
- New struct fields that represent new concepts (not just additional optional fields)
- New nested structs (e.g., `Providers map[string]map[string]string`)
- New environment variable constants

**Severity**: Stale reference (high) if the data model diagram is materially wrong. Missing coverage (medium) for new fields.

### 5. Pipeline Count

**Rule**: Count distinct data flow pipelines in the codebase:
- Push pipeline (local -> Grafana via k8s API)
- Pull pipeline (Grafana -> local via k8s API)
- Delete pipeline (local -> Grafana deletion)
- Serve pipeline (local -> browser preview)
- Provider-specific pipelines (push/pull via REST API)

Each should be documented in `data-flows.md`.

**Severity**: Missing coverage (high) for entirely new pipeline types. Low for variations of existing pipelines.

### 6. README Index

**Command**: `ls agent-docs/*.md`

**Rule**: Every `.md` file in `agent-docs/` (except README.md itself) must be listed in `agent-docs/README.md`'s navigation table.

**Severity**: Structural issue (medium) for unlisted docs. Stale reference (low) for listed docs that no longer exist.

## Doc-Specific Checks

### architecture.md
- Layered architecture diagram includes all major packages
- Core abstractions list is current
- Design decisions section covers current patterns

### patterns.md
- Pattern count matches detected patterns (see check #3)
- Each pattern has: name, confidence score, description, key files, usage context
- No patterns reference removed code

### resource-model.md
- Core types (Resource, Selector, Filter, Descriptor) descriptions are current
- Discovery system description matches current implementation
- Pipeline flow diagram is accurate

### cli-layer.md
- Command tree matches actual command groups
- Options pattern description is current
- Error handling chain description is accurate
- Exit code behavior is documented (if implemented)

### client-api-layer.md
- Client paths (dynamic + OpenAPI + provider-specific) are listed
- Auth flow description is current
- Error translation chain is accurate

### config-system.md
- Data model matches struct shapes
- Environment variable table is complete
- Loading chain description is accurate
- Provider config section exists (if providers are implemented)

### data-flows.md
- All pipeline types are documented
- Concurrency model descriptions are current
- Processor application points are accurate

### project-structure.md
- Directory layout matches actual structure
- Build targets match Makefile
- CI/CD description matches current workflows
- Dependency list covers major dependencies

## Severity Definitions

| Severity | Definition | Action |
|----------|-----------|--------|
| **Stale reference** | A documented path, type, or package no longer exists or has materially changed | Must fix -- document references wrong information |
| **Missing coverage** | A new package, command, pattern, or pipeline is undocumented | Should fix -- agents will miss important context |
| **Structural issue** | README index incomplete, metadata outdated | Nice to fix -- organizational quality |

## Update Guidelines

When updating docs to fix violations:

1. **Preserve existing style** -- Match the formatting, heading levels, and writing style of the surrounding content
2. **Add, don't rewrite** -- Insert new sections or rows; don't reorganize unaffected content
3. **Stay high-level** -- Add architecture-level descriptions, not implementation details
4. **Cross-link** -- When adding a new section, add corresponding entries in README.md and CLAUDE.md if appropriate
5. **Include confidence** -- New pattern entries in `patterns.md` should include a confidence percentage
6. **Update metadata** -- Change the `Last updated` date in `agent-docs/README.md` after any updates
