# Vision: gcx

> What gcx is, why it exists, and where it's going.

## One-Liner

Grafana Cloud and the Grafana Assistant — in your terminal and your agentic coding environment.

## The Problem

Agentic coding tools have changed how developers build software. But they're flying blind — code ships, observability comes later (if at all). Production context lives in Grafana dashboards, alert rules, and SLO definitions. Developer context lives in the editor. The two don't talk to each other.

Meanwhile, Grafana Cloud has grown into a platform with dozens of products — SLOs, Synthetic Monitoring, OnCall, Fleet Management, K6, Incidents, Knowledge Graph, Adaptive Telemetry — each with its own API, its own auth story, its own CLI gap. Managing them requires context-switching between web UIs, curl commands, and Terraform configs.

## The Solution

gcx is a single CLI that unifies access to the entire Grafana Cloud stack. It works in two tiers:

1. **K8s resource tier** — dashboards, folders, and other Grafana-native resources via Grafana 12's Kubernetes-compatible API (`k8s.io/client-go`)
2. **Cloud provider tier** — pluggable providers for every Grafana Cloud product via product-specific REST APIs

Every command serves both humans and AI agents. Agent mode is auto-detected (Claude Code, Cursor, Copilot) and switches defaults (JSON output, no color, no truncation, auto-approved prompts) without changing available functionality.

## Core Beliefs

- **Full platform coverage.** Every Grafana and Grafana Cloud feature will eventually be supported. One tool, not twenty — a developer managing SLOs, synthetic checks, alert rules, and dashboards shouldn't need four different CLIs with four different auth setups.
- **Works everywhere Grafana does.** Usable across Grafana OSS and Grafana Cloud. The same commands, the same manifests, the same workflows — only the `--context` changes.
- **Dual-purpose by design.** Humans and agents use the same commands. The CLI grammar, exit codes, and output shapes are designed for both audiences from day one — not bolted on later.
- **Easy onboarding and setup.** Getting started should take minutes, not hours. `gcx setup` guides users through connection, auth, and product configuration. Sensible defaults everywhere.
- **Consistent UX across all functionality.** Whether you're querying metrics, managing SLOs, or configuring OnCall schedules, the command grammar, output formats, flag conventions, and error messages follow the same patterns. See [DESIGN.md](DESIGN.md).
- **GitOps-native.** Pull resources to files, version in git, push back. Push is idempotent. The same manifests work across environments via `--context`. CI/CD is a first-class workflow.
- **Extensible without forking.** New Grafana Cloud products are added as providers — a self-contained package that implements an interface and self-registers. No changes to core code required.

## Grafana Assistant

The Grafana Assistant is gcx's differentiator. Where other CLIs stop at data retrieval, gcx integrates the Assistant for:

- **Automated investigation** — when an alert fires, the Assistant traces the issue, assembles context, and suggests mitigations
- **Conversational troubleshooting** — ask questions about your production environment in natural language
- **End-to-end remediation** — investigation → fix → instrumentation → monitoring, without leaving the editor

The workflow: alert fires → Assistant investigates → agent drafts fix → agent instruments with OpenTelemetry → agent creates monitoring → PR ships. Humans steer; agents execute.

## Roadmap Themes

Drawn from current milestones and open issues:

### Authentication & Identity
- OAuth browser flow with refresh token rotation
- Cloud Access Policy token support
- Token scope metadata and expiry tracking
- Unified `gcx auth login` experience

### Assistant & AI Integration
- Investigation CRUD (create, continue, summarize)
- A2A (Agent-to-Agent) protocol support
- Conversational troubleshooting via CLI

### Platform Completeness
- SCIM provider (user/team provisioning)
- GCom provider (access policies, billing, stacks)
- Cloud Migrations provider
- ML provider
- Cloud Provider integrations (AWS, Azure)
- Recording rules (Loki, Prometheus)

### Developer Experience
- Universal list filters (`--limit`, `--labels`, `--field-selectors`)
- Mutation summary tables for push/pull/delete
- Cold-start latency reduction
- Configurable HTTP client settings and `--timeout`
- Auto-generated examples from OpenAPI schemas

### Quality & Operations
- Local JSONL audit logging for mutations
- API call attribution headers
- Smoke test infrastructure with bootstrap tokens

## Release Timeline

| Milestone | Target | Focus |
|-----------|--------|-------|
| Internal Release | 2026-04-03 | Core stability, provider coverage |
| Private Preview | 2026-04-11 | UX polish, Assistant integration, provider completeness |
| Public Preview | 2026-04-20 | OAuth, platform providers, universal list filters |

## Related

- [README.md](README.md) — user-facing introduction and quick start
- [ARCHITECTURE.md](ARCHITECTURE.md) — technical architecture and ADR index
- [DESIGN.md](DESIGN.md) — CLI UX design and taste rules
- [CONSTITUTION.md](CONSTITUTION.md) — enforceable invariants
