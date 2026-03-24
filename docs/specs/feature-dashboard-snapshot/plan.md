---
type: feature-plan
title: "Dashboard Snapshot Command"
status: draft
spec: spec/feature-dashboard-snapshot/spec.md
created: 2026-03-17
---

# Architecture and Design Decisions

## Pipeline Architecture

```
User Input                    CLI Layer                         HTTP Client                     Disk I/O
─────────                     ─────────                         ───────────                     ────────
                              cmd/grafanactl/dashboards/
                              ┌──────────────────────┐
  UIDs + flags ──────────────►│  snapshotOpts        │
                              │  .setup(flags)       │
                              │  .Validate()         │
                              └──────────┬───────────┘
                                         │
                                         ▼
                              ┌──────────────────────┐
                              │  Build render URL    │
                              │  per UID (+panelId)  │
                              └──────────┬───────────┘
                                         │
                              ┌──────────▼───────────┐
                              │  errgroup (bounded)  │     internal/dashboards/
                              │  concurrency=10      │     ┌──────────────────────┐
                              │  per UID:            ├────►│  renderer.Client     │
                              │                      │     │  .Render(ctx, req)   │
                              └──────────┬───────────┘     │   GET /render/d/...  │
                                         │                 │   → []byte (PNG)     │
                                         │                 └──────────────────────┘
                                         ▼
                              ┌──────────────────────┐
                              │  Write PNG to disk   │
                              │  {uid}.png           │
                              │  {uid}-panel-{id}.png│
                              └──────────┬───────────┘
                                         │
                                         ▼
                              ┌──────────────────────┐
                              │  Output metadata     │
                              │  Agent: JSON array   │
                              │  Human: table        │
                              └──────────────────────┘
```

## Integration With Root Command

```
cmd/grafanactl/root/command.go
  rootCmd.AddCommand(dashboards.Command())   ← NEW registration
```

The `dashboards` command group follows the exact pattern of `datasources`:
- `Command()` returns a `*cobra.Command` parent
- `configOpts` bound to persistent flags
- `snapshot` registered as a subcommand

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| New `internal/dashboards/` package for render client | Separates HTTP render logic from CLI wiring, matching `internal/query/prometheus/` pattern. Enables unit testing with `httptest.Server`. (FR-002, FR-003, FR-007) |
| `renderer.Client` struct with `*http.Client` field | Mirrors `prometheus.Client` — constructed via `rest.HTTPClientFor(&cfg.Config)`, carries auth from active context. (FR-007) |
| `RenderRequest` struct for URL building | Encapsulates uid, panelId, width, height, theme, from, to, tz, orgId. Client builds query params from this struct. (FR-002, FR-003, FR-004) |
| Output via `agent.IsAgentMode()` branch, not codec system | PNG is binary — codecs are text-only. Agent mode emits JSON array to stdout; human mode emits table. Both paths use the `SnapshotResult` struct. (FR-010, FR-011) |
| `--window` validation reuses SLO pattern | `ValidateTimelineFlags` in `internal/providers/slo/definitions/timeline.go` is the established pattern. Snapshot command implements identical logic inline (no shared extraction yet — SLO one takes `*cobra.Command`). (FR-004a) |
| Default dimensions vary by mode: 1920x1080 dashboard, 800x600 panel | Set in `Validate()` — if `--panel` is set and width/height are unset, apply panel defaults. Otherwise apply dashboard defaults. (FR-004) |
| Fail-open per dashboard, non-zero exit at end | errgroup collects errors; command returns aggregated error after all UIDs are attempted. Matches risk mitigation for missing UIDs. (FR-008, FR-009) |
| `os.MkdirAll` for output directory | Called once before the errgroup starts. (FR-014) |
| File overwrite with debug log | `slog.Debug("overwriting existing snapshot", "path", path)` before `os.WriteFile`. (Negative constraint) |

## Compatibility

**Unchanged functionality:**
- All existing commands (`resources`, `datasources`, `query`, `config`, `linter`, `dev`, `providers`, `api`) are unaffected
- `manage-dashboards` skill retains all existing workflows; a new workflow section is appended
- `debug-with-grafana` skill retains all existing steps; a snapshot reference is added to Step 6

**New functionality:**
- `grafanactl dashboards` command group (currently does not exist)
- `grafanactl dashboards snapshot` subcommand
- `internal/dashboards/` package with render client

**No deprecations.**
