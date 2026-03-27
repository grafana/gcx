# Research Report: gcx `ascode` Subcommand

## Executive Summary

The proposed `dev` subcommand (renamed from `ascode`) is **feasible and fills a real gap**. The grafana-foundation-sdk is mature enough for integration (Go builder pattern, built-in converter function, zero external deps), and PR #1038's dashboard converter is merged and usable. The command group should be built into gcx as a Provider — consistent with the extensibility architecture established in the [non-app-platform extensibility research](2026-03-02-gcx-non-app-platform-extensibility.md). Splitting into a separate binary would cause auth/UX/agent-context fragmentation problems identical to those documented in that research.

**Overall Confidence: 85%**

---

## 1. Foundation SDK: Ready for Integration

| Aspect | Status |
|--------|--------|
| Go module | `github.com/grafana/grafana-foundation-sdk/go` @ v0.0.11 |
| Stability | Pre-1.0, but builder API is consistent and generated |
| Packages | 55+ (dashboard, panels, datasources, common, cog) |
| Pattern | Fluent builders: `dashboard.NewDashboardBuilder("title").Uid("x").WithPanel(...)` |
| Converter | Built into SDK: `dashboard.DashboardConverter(dash) string` |

The SDK uses a fluent builder pattern that's well-suited for code generation:

```go
dashboard.NewDashboardBuilder("My Dashboard").
    Uid("generated-from-go").
    Tags([]string{"generated", "from", "go"}).
    Refresh("1m").
    WithRow(dashboard.NewRowBuilder("Overview")).
    WithPanel(
        timeseries.NewPanelBuilder().
            Title("Network Received").
            Unit("bps").
            Min(0).
            WithTarget(
                prometheus.NewDataqueryBuilder().
                    Expr(`rate(node_network_receive_bytes_total[$__rate_interval]) * 8`),
            ),
    )
```

**Key finding**: `dashboard.DashboardConverter()` is a **generated function built into the SDK itself** — not just in the converter tool. This means gcx can import the SDK and call the converter directly as a library, no external tool needed.

---

## 2. Dashboard Converter (PR #1038): Merged & Usable

PR #1038 was **merged on 2026-02-11**. It open-sources a tool created by Grafana's platform-monitoring team.

### Architecture (two layers)

```
Layer 1: SDK built-in (importable as library)
┌──────────────────────────────────────────┐
│ dashboard.DashboardConverter(Dashboard)  │
│ → returns Go builder code as string      │
│ (generated code in the SDK)              │
└──────────────────────────────────────────┘

Layer 2: CLI wrapper (scripts/dashboard-converter/)
┌──────────────────────────────────────────┐
│ JSON file/stdin                          │
│ → cleanup (remove nulls, empty strings)  │
│ → unmarshal to Dashboard struct          │
│ → call DashboardConverter()              │
│ → wrap in main.go template               │
│ → goimports                              │
│ → file/stdout                            │
└──────────────────────────────────────────┘
```

**For gcx**: Import the SDK, call `DashboardConverter()` directly. Replicate the ~50-line cleanup logic. Use your own template instead of their `main.go.tmpl`. No dependency on the CLI tool.

---

## 3. Naming Analysis

### Existing terminology in the ecosystem

| Tool | Term Used | Scaffolding Command |
|------|-----------|-------------------|
| Grafana docs | "as code" | N/A |
| Foundation SDK | "builder libraries" | None |
| kubebuilder | N/A | `init` / `create api` |
| operator-sdk | N/A | `init` / `create api` |
| Pulumi | N/A | `new` |
| CDK | N/A | `init` |
| Helm | N/A | `create` |

### Naming options ranked

| Name | Pros | Cons |
|------|------|------|
| **`dev`** | Short, clear intent (development workflow), matches `skaffold dev` / `tilt up` pattern | Generic |
| **`sdk`** | Direct reference to foundation-sdk | Confusing — users don't interact with "an SDK", they write dashboards |
| **`codegen`** | Accurate for generate/import | Doesn't fit scaffold/serve |
| **`ascode`** | Matches Grafana's "as code" terminology | Unusual as a CLI noun, slightly awkward to type |
| **`code`** | Shorter version of ascode | Extremely generic |
| **`generate`** | Clear for codegen | Clashes with `go generate`, doesn't fit serve |
| **`project`** | Clear for scaffold | Doesn't fit import/serve |

**Recommendation**: **`dev`** or **`ascode`**

- `dev` wins on ergonomics: `gcx dev init`, `gcx dev serve`, `gcx dev import`
- `ascode` wins on brand alignment with Grafana's "as code" messaging
- Either works. Avoid `sdk`, `codegen`, or `generate` — they don't cover the full command surface.

---

## 4. Architectural Assessment

### Why NOT a separate binary

The initial analysis considered the kubectl/kubebuilder precedent (separate binaries for runtime vs. dev tooling). However, gcx's context is different from kubectl's — and the [extensibility research](2026-03-02-gcx-non-app-platform-extensibility.md) already settled this question for product providers. The same arguments apply to dev tooling:

1. **Auth Problem** — `dev import` needs `internal/config` to fetch dashboards from the Grafana API. A separate binary can't import `internal/` packages. Re-implementing config loading (~200 LOC) for a plugin is wasteful.

2. **UX Divergence** — Separate binary loses `fail.DetailedError`, `cmdio`, Options pattern, output codecs. Plugin CLIs inevitably diverge in flag naming, error formatting, and output styles.

3. **Agent Context Fragmentation** — An agent implementing `dev generate` in a separate repo loses all context from agent-docs, CLAUDE.md, and existing patterns. In the monorepo, it's "read the provider guide, follow the template."

4. **Dependency "bloat" is negligible** — The foundation SDK has **zero external dependencies** (`go.mod` contains only `go 1.21`). The converter adds `golang.org/x/tools` for goimports. This is trivial compared to gcx's existing 100+ line `go.mod`.

### Recommended architecture: Built-in Provider

```
internal/providers/
  provider.go       ← Provider interface
  registry.go       ← All() registration
  slo/              ← product provider (API client)
  synth/            ← product provider (API client)
  dev/              ← dev tooling provider
    provider.go     ← DevProvider implementing Provider
    scaffold.go     ← init command (scaffold Go project)
    generate.go     ← generate command (add dashboard file)
    import.go       ← import command (JSON → Go via DashboardConverter)
    templates/      ← embedded Go templates for scaffolding
```

This follows the same Provider pattern as SLO/synth/k6. The `dev` provider just happens to be dev tooling rather than a product API client.

### The `serve` question

`resources serve` already supports foundation-sdk projects via `--script 'go run .'`. Two options:

1. **Keep serve in `resources`** — `gcx dev serve` is a convenience alias that auto-detects Go projects and calls `resources serve` with the right defaults
2. **Move serve under `dev`** — since the serve workflow is primarily a dev concern

**Recommendation**: Option 1. Keep the serve infrastructure where it is, add a `dev serve` alias that sets `--script 'go run .' --watch . --script-format json` as defaults.

---

## 5. Proposed Command Design

```
gcx dev init [--module=github.com/org/dashboards]
  → Scaffold a new Go project with:
    - go.mod (with foundation-sdk dependency)
    - main.go (entry point that outputs JSON)
    - dashboards/ directory
    - Makefile (build, run, fmt)

gcx dev generate dashboard <name> [--destination=<file>]
  → Add a new dashboard Go file using builder template
  → Creates dashboards/<name>.go with empty builder scaffold

gcx dev import dashboard <name> [--from-uid=<uid>] [--from-file=<json>]
  → Fetch dashboard JSON from Grafana API (or file)
  → Run DashboardConverter() → produce Go builder code
  → Write to dashboards/<name>.go

gcx dev serve [same flags as resources serve]
  → Convenience wrapper: auto-detects Go project, runs 'go run .',
    watches for changes, serves locally
  → Delegates to resources serve --script 'go run .' internally
```

---

## 6. Gap Analysis & Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| SDK pre-1.0 breakage | Medium | Pin version, test on upgrade |
| Converter doesn't handle all panel types | Low | Generated code covers all types in SDK |
| `resources serve` duplication if moved | Low | Don't move — wrapper pattern |
| Naming bikeshed | Low | `dev` chosen — iterate later if needed |

### No existing scaffolding tool found
There is **no existing CLI** in the Grafana ecosystem for scaffolding foundation-sdk projects. The examples in the repo are bare Go files. This is a genuine gap that gcx would fill.

---

## Sources

1. [grafana/grafana-foundation-sdk](https://github.com/grafana/grafana-foundation-sdk) — repo structure, Go README, go.mod (via `gh api`)
2. [PR #1038: Open source dashboard-converter tool](https://github.com/grafana/grafana-foundation-sdk/pull/1038) — merged 2026-02-11 (via `gh pr view`)
3. `scripts/dashboard-converter/converter.go` — converter source code (via `gh api`)
4. `go/dashboard/dashboard_converter_gen.go` — SDK built-in converter function (via `gh api`)
5. `scripts/dashboard-converter/README.md` — converter docs (via `gh api`)
6. [Kubernetes Plugin Architecture](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/) — kubectl plugin naming convention
7. gcx source: `cmd/gcx/resources/serve.go` — existing serve implementation (local file)
8. [Non-App-Platform Extensibility Research](2026-03-02-gcx-non-app-platform-extensibility.md) — Provider interface architecture decision (2026-03-02)
