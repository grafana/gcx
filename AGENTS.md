# gcx — Agent & Developer Entry Point

> Lightweight map for autonomous coding agents. Read this first, then navigate to specific docs on demand.

## Quick Start

**gcx** is a unified CLI for managing Grafana resources. It operates in two tiers: (1) a **K8s resource tier** that uses Grafana 12+'s Kubernetes-compatible API via `k8s.io/client-go` for dashboards, folders, and other K8s-native resources, and (2) a **Cloud provider tier** with pluggable providers for Grafana Cloud products (SLO, Synthetic Monitoring, IRM, Fleet Management, etc.) that use product-specific REST APIs. Built in Go, it uses Cobra for CLI structure.

## Documentation Map

| File | Purpose |
|------|---------|
| [VISION.md](VISION.md) | Product fit: goals, core beliefs, product surface |
| [ARCHITECTURE.md](ARCHITECTURE.md) | System overview and architecture rules: layers, resource pipeline, providers, transport |
| [DESIGN.md](DESIGN.md) | CLI and agent-facing rules: command grammar, output contract, exit codes, safety |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contribution workflow, code conventions, dev setup, test environment |
| [README.md](README.md) | Product tour for gcx users |
| [docs/architecture/](docs/architecture/) | Deep-dive architecture docs (patterns, resource model, CLI layer, data flows, …) |
| [docs/design/](docs/design/) | Prescriptive UX implementation rules (output, errors, agent mode, naming, …) |
| [docs/rfcs/](docs/rfcs/README.md) | Engineering proposals, design tradeoffs, and open questions |
| [docs/sources/](docs/sources/) | Public user documentation, published to the Grafana website |
| [docs/contributing/](docs/contributing/) | Contributor guides, including [documentation maintenance](docs/contributing/doc-maintenance.md) |
| [docs/reference/](docs/reference/) | Provider guides and references being re-homed into `docs/sources/` and `docs/contributing/`; add nothing new here |

## Architecture at a Glance

Two tiers: **K8s resource tier** (dashboards, folders via `/apis`) and **Cloud provider tier** (SLO, SM, IRM, etc. via product REST APIs). See [ARCHITECTURE.md](ARCHITECTURE.md) for pipeline diagrams and extension pipelines.

## Key Conventions

The rules live in [ARCHITECTURE.md § Rules](ARCHITECTURE.md#rules), [DESIGN.md](DESIGN.md) and [CONTRIBUTING.md § Rules and code conventions](CONTRIBUTING.md#rules-and-code-conventions). Read the relevant one before writing code. The conventions below are specific to agents.

- **Skill placement follows the audience.** Portable workflows for people using gcx live in `claude-plugin/skills/` and ship in the binary; repository-only contributor workflows live in `.agents/skills/`. A contributor-only workflow in the bundle ships to every gcx user.
- **`.claude/skills` is a Git-tracked relative symlink to `../.agents/skills`.** Edit the files in `.agents/skills/`; a copy in place of the symlink lets the two trees drift. `mise run validate-skills` checks the link.
- **Skills only reference commands and flags that exist.** CI checks every `gcx` invocation in both skill trees against the real command tree.
- **Bundled skills are registered for their whole lifecycle.** Register each one in `claude-plugin/skills-catalog.yaml` and append it to `internal/skills/testdata/shipped_skills.txt`. Never delete a name from either file: retired skills stay in the catalog so old installations can remove them, and the test does not catch a name deleted from both. See [skill lifecycle](claude-plugin/README.md#skill-lifecycle).

## Essential Commands

```bash
mise run build       # Build to bin/gcx
mise run tests       # Run all tests with race detection
mise run lint        # Run golangci-lint
mise run gate        # fast pre-push gate for code changes; no Docker needed
mise run all         # every local check, including Docker image and docs
mise run docs        # Build Grafana website docs (requires Docker)
```

**Without mise**: replace with direct Go commands — `go build -buildvcs=false -o bin/gcx ./cmd/gcx/` and `go test ./...`. Always build to `bin/gcx`. Lint runs in Go **module mode** (`golangci-lint`'s `modules-download-mode: readonly`), so no `vendor/` directory is needed locally — the module cache (`go mod download`, run automatically on worktree entry) is sufficient.

> **Agent environments**: the test tasks and release-reference generator pin `GCX_AGENT_MODE=false` themselves. Normal docs builds validate `docs/sources` without regenerating the released CLI reference.

## Testing

New behavior ships with tests; prefer table-driven tests. See [CONTRIBUTING.md § Rules and code conventions](CONTRIBUTING.md#rules-and-code-conventions).

## Source navigation

Start with [project structure](docs/architecture/project-structure.md) for module boundaries. Browse `cmd/gcx/` for command wiring and `internal/` for implementation; use `bin/gcx help-tree` for the current command surface.

## What to Read Before You Start

| Task | Read first | Then |
|------|-----------|------|
| **Adding a new command** | [docs/design/command-naming.md](docs/design/command-naming.md) (verb + placement), [DESIGN.md](DESIGN.md) (grammar, output model) | [docs/design/](docs/design/) for implementation rules, [ARCHITECTURE.md](ARCHITECTURE.md) § CLI layer |
| **Adding a new provider** | [ARCHITECTURE.md](ARCHITECTURE.md) § Provider System | [docs/reference/provider-guide.md](docs/reference/provider-guide.md), [docs/design/provider-checklist.md](docs/design/provider-checklist.md) |
| **Adding a signal provider command** | [ARCHITECTURE.md](ARCHITECTURE.md) § Signal Providers | Existing signal provider code for the SharedOpts pattern |
| **Modifying resource handling** | [ARCHITECTURE.md](ARCHITECTURE.md) § Resources Pipeline | [docs/architecture/resource-model.md](docs/architecture/resource-model.md), [docs/architecture/data-flows.md](docs/architecture/data-flows.md) |
| **Changing config or auth** | [ARCHITECTURE.md](ARCHITECTURE.md) § Configuration + § Auth | [docs/architecture/config-system.md](docs/architecture/config-system.md), [docs/architecture/client-api-layer.md](docs/architecture/client-api-layer.md) |
| **Fixing a bug** | [ARCHITECTURE.md](ARCHITECTURE.md) for the relevant subsystem | Jump directly to the deep-dive doc for that domain |
| **Creating or revising an RFC** | [RFC conventions](docs/rfcs/README.md), repo-local [create-rfc](.agents/skills/create-rfc/SKILL.md) | Use [update-rfc](.agents/skills/update-rfc/SKILL.md) when explicitly asked to reconcile verified implementation evidence |
| **Reviewing an RFC** | Repo-local [review-rfc](.agents/skills/review-rfc/SKILL.md), the originating request and [RFC conventions](docs/rfcs/README.md) | Check claims, scope, repository rules, consistency and unresolved questions before sharing or acceptance |
| **Planning work before implementation** | Repo-local [`contribute`](.agents/skills/contribute/SKILL.md) and the [contribution workflow](CONTRIBUTING.md#contributing-code) | Load the proposal or RFC specialist only when the selected route needs it; keep restricted research outside the checkout |
| **Planning a new feature** | [VISION.md](VISION.md) (does it belong?) | [DESIGN.md](DESIGN.md) and [ARCHITECTURE.md § Rules](ARCHITECTURE.md#rules) (can we build it within the rules?) |
| **Adding, extending, or reviewing a gcx capability (domain teams)** | Repo-local [`contribute`](.agents/skills/contribute/SKILL.md) — necessity, placement, readiness, contract, implementation references, and self-review | [docs/design/command-naming.md](docs/design/command-naming.md), [docs/reference/provider-guide.md](docs/reference/provider-guide.md) |
| **Reviewing someone else's PR** | The repo-local [`review-pr`](.agents/skills/review-pr/SKILL.md) skill — report shape, over-engineering rubric, lock-in ranking | [Compliance Hierarchy](#compliance-hierarchy) below — check every level in order |

## Compliance Hierarchy

Check work against these documents while planning, designing and implementing, in this order.

| # | Doc | Strictness | What to check | If violated |
|---|-----|-----------|---------------|-------------|
| 1 | [VISION.md](VISION.md) | **Product fit** — violation is wasted work | Does this belong in gcx? Does it serve the core beliefs? | Pause. Confirm direction with a human before investing more effort. |
| 2 | [ARCHITECTURE.md § Rules](ARCHITECTURE.md#rules) and [DESIGN.md](DESIGN.md) | **Rules** — violation is a bug | Layering, pipeline, provider registration, transport, command grammar, output contract, exit codes, safety | Fix before proceeding. Existing violations are tracked debt, not precedent. Waiving a rule needs maintainer approval in the PR. |
| 3 | [CONTRIBUTING.md](CONTRIBUTING.md#rules-and-code-conventions) | **Code conventions** — violation is a review defect | Options pattern, tests, dependencies, linter suppressions, commit titles | Fix. |
| 4 | [docs/design/](docs/design/) | **Design mechanics** — violation is a UX defect | Command naming, codecs, errors, exit codes, safety, provider UX checklist | Fix. New code must comply. |
| 5 | [docs/architecture/](docs/architecture/README.md) | **Architecture mechanics** — how to follow the architecture rules | Patterns, data flows, client layer | Prefer compliance. A deviation needs its rationale in the PR or an RFC. |

**When to check:**
- **Planning/design**: VISION (1) and the rules (2) — are we building the right thing, and can we build it within the rules?
- **Implementation**: the rules (2), conventions (3) and mechanics (4, 5).
- **Pre-flight** (below): a final sweep across every level before pushing.

## Releasing

Release steps live in the `release` skill ([.agents/skills/release/SKILL.md](.agents/skills/release/SKILL.md)) — invoke it when tagging a release.

## Mandatory Pull Request Checklist

You MUST run this checklist when creating a PR or updating an existing PR with new work (addressing PR reviews or fixing bugs). This is distinct from the Mandatory Pre-Commit Checklist below — `mise run all` in step 3 subsumes the individual pre-commit steps; do not substitute the pre-commit checklist here.

1. **Compliance check** — verify changes against the [compliance hierarchy](#compliance-hierarchy) above. Rule and convention violations must be fixed. VISION misalignment must be flagged. Deviations from the mechanics must be documented.
2. **Sync with base branch**
   ```bash
   git fetch origin main && git rebase origin/main
   ```
3. **Quality gates pass** (Docker is required for the Grafana docs build):
   ```bash
   mise run all
   ```
4. **Doc maintenance gate** — follow [documentation maintenance](docs/contributing/doc-maintenance.md). Update guidance affected by changed behavior, architectural boundaries, or contributor workflows; do not mirror package lists or proposal inventories.
5. **PR body follows the [template](.github/pull_request_template.md)** — the sections `### What`, `### Why`, `### How it was tested` and `### PR checklist`, in that order, with every checklist item answered. The title follows [Conventional Commits](CONTRIBUTING.md#rules-and-code-conventions).
6. **Push**
   ```bash
   git push
   git status   # must show "up to date with origin"
   ```
   Work is not done until push succeeds. If it fails, resolve and retry.

## Mandatory Pre-Commit Checklist

Run this checklist **before every commit** (not only before PR/push):

1. **Format touched files**
   ```bash
   gofmt -w <touched-go-files>
   ```
2. **Lint passes**
   ```bash
   mise run lint
   ```
3. **Targeted tests pass** for changed packages
   ```bash
   go test ./path/to/changed/package/...
   ```
4. **Full test suite passes**
   ```bash
   go test ./...
   ```
5. **Released reference stays separate** — do not regenerate `docs/sources/cli-reference.md`
   in command PRs. `mise run tag` refreshes references in the release changelog PR;
   use `mise run docs:refresh` for standalone updates to the latest stable release.
   Test generator changes with `go test ./scripts/...`.
6. **Docs build succeeds** (Docker required; CI preserves the `Documentation` check)
   ```bash
   mise run docs
   ```
7. **No unstaged surprises**
   ```bash
   git status
   ```

## GitHub Issues

When creating or commenting on GitHub issues, **always anonymize system-specific details**. Replace real values with placeholders:

- Stack names / context names → `<my-context>`, `<stack>`
- URLs with stack or region identifiers → `https://example-<region>.grafana.net`
- Hosted IDs, stack IDs, org IDs → `12345`, `99999`
- Datasource names with stack slugs → `grafanacloud-<stack>-prom`
- API tokens, credentials → never include, even partially

This applies to issue bodies, comments, and code snippets embedded in issues.

## Security

Report security vulnerabilities through [Grafana's security issue reporting page](https://grafana.com/legal/report-a-security-issue/). Keep vulnerability reports private; do not file them as GitHub issues in this repository. Ordinary security-related engineering work, such as hardening and dependency maintenance, follows the normal issue workflow.
