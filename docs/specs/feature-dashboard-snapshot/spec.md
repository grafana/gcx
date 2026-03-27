---
type: feature-spec
title: "Dashboard Snapshot Command"
status: done
created: 2026-03-17
---

# Dashboard Snapshot Command

## Problem Statement

Agents and CLI users have no way to capture visual snapshots (PNG images) of Grafana dashboards or individual panels via gcx. The only option today is to manually open Grafana in a browser and take screenshots, which is not automatable. Agents working with gcx cannot "see" what a dashboard looks like, limiting their ability to assist with dashboard debugging, layout review, and visual regression detection. The `gcx dashboards snapshot` command will call the Grafana Image Renderer API to download PNG images and expose file paths to agents via structured output.

## Scope

### In Scope

- New `dashboards` command group under `cmd/gcx/dashboards/`
- `gcx dashboards snapshot` subcommand that renders full dashboards and individual panels to PNG
- HTTP client integration using existing `rest.HTTPClientFor` pattern (Pattern 12) for auth
- Configurable render parameters: width, height, theme, time range, timezone, panelId
- File output: write PNG images to a specified directory with deterministic naming
- Agent-mode structured output: JSON with file paths and metadata for agent consumption
- Human-mode table output: summary of rendered snapshots with file paths
- Concurrent rendering of multiple dashboards via errgroup with bounded parallelism
- Update `claude-plugin/skills/manage-dashboards/SKILL.md` with snapshot workflow
- New `claude-plugin/skills/debug-with-grafana` references for snapshot usage in diagnostics

### Out of Scope

- **PDF export** — Grafana Image Renderer only supports PNG; PDF is a separate Grafana Enterprise feature
- **Animated GIF or video capture** — not supported by the render API
- **Dashboard diff via image comparison** — visual diffing is a separate feature that could consume snapshots later
- **Image Renderer plugin installation or health checking** — users must ensure the renderer is installed; gcx will not manage Grafana plugins
- **Inline image display in terminal** — PNG files are written to disk; terminal image protocols (iTerm2, Kitty) are not in scope
- **Batch rendering all dashboards in a namespace** — first iteration targets explicit dashboard UIDs only
- **Dashboard slug resolution via API** — the render endpoint accepts UID directly with an empty slug; slug resolution is unnecessary

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Command location | `gcx dashboards snapshot` under new `dashboards` group | Dashboards are a first-class Grafana concept; snapshot is a dashboard-specific operation. The `dashboards` group can host future subcommands (e.g., `dashboards lint`). | Codebase: datasources command group pattern |
| Render URL slug | Use empty slug (`/render/d/{uid}/` with trailing slash) | Grafana Image Renderer accepts empty slugs; avoids needing to resolve dashboard title to slug. | Grafana Image Renderer API |
| HTTP client | `rest.HTTPClientFor(&cfg.Config)` from existing config | Reuses auth (API key, service account token, TLS) from the active gcx context. No new auth mechanism needed. | Pattern 12 in codebase |
| Output format | Direct PNG file write + JSON metadata (not codec pipeline) | PNG is binary; standard JSON/YAML codecs cannot encode it. The command writes PNG to disk and outputs metadata about the written files. | Codebase constraint: codecs are text-only |
| File naming | `{dashboard-uid}.png` or `{dashboard-uid}-panel-{panelId}.png` | Deterministic, filesystem-safe, and predictable for agents. | Agent consumption requirement |
| Concurrency | errgroup with bounded parallelism (default 10) | Matches existing codebase pattern for batch I/O. Prevents overwhelming the renderer. | Codebase pattern |
| Default dimensions | 1920x1080 (full dashboard), 800x600 (single panel) | Common screen resolution for dashboards; reasonable panel size. User-overridable. | Grafana Image Renderer defaults |
| Time range flags | `--from`/`--to`/`--window` matching `query` and `slo reports timeline` | Consistency across commands; `--window` shorthand already established in SLO provider. Same format: RFC3339, Unix ts, relative ("now-1h"). | Codebase: query/command.go, slo/reports/timeline.go |

## Functional Requirements

FR-001: The CLI MUST provide a `gcx dashboards snapshot` command that accepts one or more dashboard UIDs as positional arguments.

FR-002: The command MUST render each specified dashboard to a PNG image by calling `GET /render/d/{uid}/?orgId={orgId}&width={width}&height={height}` on the configured Grafana instance.

FR-003: The command MUST support a `--panel` flag that, when provided, renders a single panel by calling `GET /render/d-solo/{uid}/?orgId={orgId}&panelId={id}&width={width}&height={height}`.

FR-004: The command MUST support the following optional flags: `--width` (int, default 1920 for dashboard / 800 for panel), `--height` (int, default 1080 for dashboard / 600 for panel), `--theme` (string, "light" or "dark", default "dark"), `--from` (string, relative or absolute time, same format as `query --from` and `slo reports timeline --from`: RFC3339, Unix timestamp, or relative like "now-1h"), `--to` (string, same format, default "now"), `--window` (string, time window shorthand e.g. "1h", "7d" — equivalent to `--from now-{window} --to now`, mutually exclusive with explicit `--from`/`--to`, matching the `slo reports timeline --window` pattern), `--tz` (string, timezone), `--org-id` (int, default 1).

FR-004a: When `--window` is provided alongside `--from` or `--to`, the command MUST return a validation error. When `--window` is provided alone, it MUST expand to `--from now-{window} --to now`.

FR-005: The command MUST write PNG files to the directory specified by `--output-dir` (default: current working directory).

FR-006: The command MUST name output files as `{dashboard-uid}.png` for full dashboard snapshots and `{dashboard-uid}-panel-{panelId}.png` for single panel snapshots.

FR-007: The command MUST authenticate requests using the HTTP client derived from the active gcx context configuration via `rest.HTTPClientFor`.

FR-008: The command MUST return a non-zero exit code when any render request fails (HTTP non-200, network error, or empty response body).

FR-009: When multiple dashboard UIDs are provided, the command MUST render them concurrently using errgroup with bounded parallelism (default 10, configurable via `--concurrency`).

FR-010: In agent mode (detected via `agent.IsAgentMode()`), the command MUST output JSON to stdout containing an array of snapshot results, each with fields: `uid`, `panel_id` (null if full dashboard), `file_path` (absolute), `width`, `height`, `theme`, and `rendered_at` (RFC3339).

FR-011: In human mode (non-agent, non-piped), the command MUST output a table to stdout summarizing rendered snapshots with columns: UID, Panel, File, Size.

FR-012: The command MUST follow the Options pattern: an opts struct with `setup(flags)`, `Validate()`, and a constructor function.

FR-013: The command MUST validate that at least one dashboard UID is provided, returning an error message if none are given.

FR-014: The command MUST create the output directory (including parents) if it does not exist.

FR-015: The `claude-plugin/skills/manage-dashboards/SKILL.md` MUST be updated with a new "Workflow: Capture Dashboard Snapshots" section documenting the `gcx dashboards snapshot` command usage.

FR-016: The `claude-plugin/skills/debug-with-grafana/SKILL.md` MUST be updated to reference dashboard snapshots as a diagnostic step (e.g., "capture a visual snapshot of the dashboard to inspect layout and panel state").

## Acceptance Criteria

- GIVEN a configured gcx context pointing to a Grafana instance with Image Renderer installed
  WHEN the user runs `gcx dashboards snapshot <uid>`
  THEN a file named `<uid>.png` MUST be written to the current directory containing a valid PNG image

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --panel 42`
  THEN a file named `<uid>-panel-42.png` MUST be written to the current directory containing a valid PNG image of panel 42

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --output-dir ./snapshots`
  THEN the `./snapshots` directory MUST be created if it does not exist and the PNG file MUST be written inside it

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --width 1000 --height 500 --theme light --from now-1h --to now --tz UTC`
  THEN the render request MUST include query parameters `width=1000`, `height=500`, `theme=light`, `from=now-1h`, `to=now`, `tz=UTC`

- GIVEN agent mode is active
  WHEN the user runs `gcx dashboards snapshot <uid1> <uid2>`
  THEN stdout MUST contain a JSON array with two objects, each containing `uid`, `file_path` (absolute path), `width`, `height`, `theme`, and `rendered_at` fields

- GIVEN the user provides multiple dashboard UIDs
  WHEN the user runs `gcx dashboards snapshot <uid1> <uid2> <uid3>`
  THEN all three dashboards MUST be rendered concurrently and three PNG files MUST be written

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --window 6h`
  THEN the render request MUST include query parameters `from=now-6h` and `to=now`

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --window 6h --from now-2h`
  THEN the command MUST exit with a non-zero code and print a validation error indicating that `--window` is mutually exclusive with `--from`/`--to`

- GIVEN no dashboard UID is provided
  WHEN the user runs `gcx dashboards snapshot`
  THEN the command MUST exit with a non-zero code and print an error message indicating that at least one dashboard UID is required

- GIVEN the Grafana instance does not have Image Renderer installed
  WHEN the user runs `gcx dashboards snapshot <uid>`
  THEN the command MUST exit with a non-zero code and report the HTTP error from Grafana (typically 500 or "panel plugin not found")

- GIVEN a valid render request
  WHEN the Grafana API returns an HTTP response with a non-200 status code
  THEN the command MUST exit with a non-zero code and include the HTTP status code and response body excerpt in the error message

- GIVEN the `manage-dashboards` skill file exists
  WHEN an agent reads `claude-plugin/skills/manage-dashboards/SKILL.md`
  THEN it MUST contain a workflow section for capturing dashboard snapshots with example commands

- GIVEN the `debug-with-grafana` skill file exists
  WHEN an agent reads `claude-plugin/skills/debug-with-grafana/SKILL.md`
  THEN it MUST reference the `gcx dashboards snapshot` command as a diagnostic tool

## Negative Constraints

- The command MUST NEVER write PNG data to stdout; PNG binary data MUST only be written to files on disk.
- The command MUST NEVER embed auth credentials (API keys, tokens) in log messages or error output.
- The command MUST NEVER silently overwrite existing files without notification; if a file already exists at the target path, the command MUST overwrite it (latest snapshot wins) but MUST log a debug-level message noting the overwrite.
- The command MUST NEVER add external dependencies beyond the Go standard library and existing project dependencies (`k8s.io/client-go`, `cobra`, `slog`).
- The command MUST NOT use the standard JSON/YAML codec pipeline for PNG output; codecs are for structured text data only.
- The command MUST NEVER hang indefinitely on a slow renderer; HTTP requests MUST respect the context deadline (propagated from cobra command context).

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Image Renderer not installed on target Grafana | Command fails with unhelpful error | Detect common error patterns (500, "plugin not found") and surface a clear message suggesting the user install `grafana-image-renderer` |
| Renderer timeout on complex dashboards | Command hangs or returns partial image | Use context with timeout; document that complex dashboards may need larger timeouts via `--timeout` flag or context deadline |
| Large PNG files for high-resolution renders | Disk space usage, slow agent consumption | Default to reasonable dimensions (1920x1080); document that very large dimensions produce proportionally large files |
| Dashboard UID does not exist | HTTP 404 from render endpoint | Report the UID that failed and continue rendering remaining dashboards (fail-open per dashboard, non-zero exit at end) |
| Grafana instance behind reverse proxy that strips render paths | Render endpoint unreachable | Document that `/render/` path must be accessible; no mitigation in gcx itself |

## Open Questions

- [RESOLVED] Whether to use `/render/d/{uid}/{slug}` or `/render/d/{uid}/` — the render endpoint works with an empty slug, so no slug resolution is needed.
- [RESOLVED] Whether to support `--output-format png` via the codec system — PNG is binary and cannot use text codecs; direct file write with JSON metadata is the correct approach.
- [DEFERRED] Whether to add a `gcx dashboards list` command alongside snapshot — useful but separate scope; the `resources get dashboards` command already lists dashboards.
- [DEFERRED] Whether to support rendering dashboards by title (fuzzy match) instead of UID — adds complexity; UIDs are stable identifiers and already used throughout gcx.
- [NEEDS CLARIFICATION] Whether `--org-id` default of 1 is acceptable for all deployment models, or whether it should be read from the gcx context configuration.
