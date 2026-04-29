# Vision: gcx

> What gcx is and why it exists.

## One-Liner

Grafana and the Grafana Assistant — in your terminal and your agentic coding environment. Works with Grafana Cloud, Enterprise, and OSS (Grafana 12+).

## The Problem

Agentic coding tools have changed how developers build software. But they're flying blind — code ships, observability comes later (if at all). Production context lives in Grafana dashboards, alert rules, and SLO definitions. Developer context lives in the editor. The two don't talk to each other.

Meanwhile, Grafana Cloud has grown into a platform with dozens of products — SLOs, Synthetic Monitoring, OnCall, Fleet Management, k6, Incidents, Knowledge Graph, Adaptive Telemetry — each with its own API, its own auth story, its own CLI gap. Managing them requires context-switching between web UIs, curl commands, and Terraform configs.

## The Solution

gcx is a single CLI that unifies access to the entire Grafana stack — across OSS, Enterprise, and Cloud. It works in two tiers:

1. **K8s resource tier** — dashboards, folders, and other Grafana-native resources via Grafana 12's Kubernetes-compatible API (`k8s.io/client-go`)
2. **Cloud provider tier** — pluggable providers for every Grafana Cloud product via product-specific REST APIs

Every command serves both humans and AI agents. Agent mode is auto-detected (Claude Code, Cursor, Copilot) and switches defaults (JSON output, no color, no truncation, auto-approved prompts) without changing available functionality.

## Core Beliefs

- **Terminal-first alternative to the Grafana UI.** Everything you can do in the web UI, you should be able to do from your terminal. For humans who prefer the CLI and for agents that can't use a browser, gcx is the primary interface to Grafana.
- **Full platform coverage.** Every Grafana and Grafana Cloud feature will eventually be supported. One tool, not twenty — a developer managing SLOs, synthetic checks, alert rules, and dashboards shouldn't need four different CLIs with four different auth setups.
- **Works everywhere Grafana does.** Usable across Grafana OSS, Enterprise, and Grafana Cloud. The same commands, the same manifests, the same workflows — only the `--context` changes.
- **Dual-purpose by design.** Humans and agents use the same commands. The CLI grammar, exit codes, and output shapes are designed for both audiences from day one — not bolted on later.
- **Easy onboarding and setup.** Getting started should take minutes, not hours. `gcx setup` guides users through connection, auth, and product configuration. Sensible defaults everywhere.
- **Consistent UX across all functionality.** Whether you're querying metrics, managing SLOs, or configuring OnCall schedules, the command grammar, output formats, flag conventions, and error messages follow the same patterns. See [DESIGN.md](DESIGN.md).
- **GitOps-native.** Pull resources to files, version in git, push back. Push is idempotent. The same manifests work across environments via `--context`. CI/CD is a first-class workflow.
- **Extensible without forking.** New Grafana Cloud products are added as providers — a self-contained package that implements an interface and self-registers. No changes to core code required.

## Observability as Code

The `gcx dev` commands provide an end-to-end workflow for managing Grafana resources as Go code using the [grafana-foundation-sdk](https://github.com/grafana/grafana-foundation-sdk) — scaffold, import, lint, live-preview, and push. Developer tooling generates the same manifests that the `gcx resources` pipeline and GitOps workflows consume. See [ARCHITECTURE.md § Observability as Code](ARCHITECTURE.md#6-observability-as-code-gcx-dev) for the full workflow.

## Grafana Assistant

The Grafana Assistant is gcx's differentiator. Where other CLIs stop at data retrieval, gcx integrates the Assistant for:

- **Automated investigation** — when an alert fires, the Assistant traces the issue, assembles context, and suggests mitigations
- **Conversational troubleshooting** — ask questions about your production environment in natural language
- **End-to-end remediation** — investigation → fix → instrumentation → monitoring, without leaving the editor

The workflow: alert fires → Assistant investigates → agent drafts fix → agent instruments the code → agent creates monitoring → PR ships.

## Related

- [README.md](README.md) — user-facing introduction and quick start
- [ARCHITECTURE.md](ARCHITECTURE.md) — technical architecture and ADR index
- [DESIGN.md](DESIGN.md) — CLI UX design and taste rules
- [CONSTITUTION.md](CONSTITUTION.md) — enforceable invariants
