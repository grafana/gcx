---
type: feature-tasks
title: "Dashboard Snapshot Command"
status: draft
spec: spec/feature-dashboard-snapshot/spec.md
plan: spec/feature-dashboard-snapshot/plan.md
created: 2026-03-17
---

# Implementation Tasks

## Dependency Graph

```
T1 (render client) ──┐
                      ├──► T3 (CLI command) ──► T4 (root registration + tests)
T2 (result types)  ──┘                                    │
                                                           ▼
                                                    T5 (skill docs)
```

## Wave 1: Core Library

### T1: Render Client
**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Implement `internal/dashboards/renderer.go` containing the `Client` struct and `Render` method. The client accepts a `config.NamespacedRESTConfig`, creates an `*http.Client` via `rest.HTTPClientFor`, and performs `GET /render/d/{uid}/` or `GET /render/d-solo/{uid}/` depending on whether a panel ID is specified. Returns the raw PNG bytes.

Also implement `internal/dashboards/renderer_test.go` with table-driven tests using `httptest.Server` to verify URL construction, query parameter encoding, error handling for non-200 responses, and empty body detection.

**Deliverables:**
- `internal/dashboards/renderer.go`
- `internal/dashboards/renderer_test.go`

**Acceptance criteria:**
- GIVEN a RenderRequest with uid "abc" and no panel ID
  WHEN Client.Render is called
  THEN the HTTP request MUST be sent to `/render/d/abc/?orgId=1&width=1920&height=1080`

- GIVEN a RenderRequest with uid "abc" and panel ID 42
  WHEN Client.Render is called
  THEN the HTTP request MUST be sent to `/render/d-solo/abc/?orgId=1&panelId=42&width=800&height=600`

- GIVEN a RenderRequest with from="now-1h", to="now", tz="UTC", theme="light"
  WHEN Client.Render is called
  THEN the HTTP request URL MUST include query parameters `from=now-1h`, `to=now`, `tz=UTC`, `theme=light`

- GIVEN the Grafana API returns HTTP 500
  WHEN Client.Render is called
  THEN it MUST return an error containing the HTTP status code and response body excerpt

- GIVEN the Grafana API returns HTTP 200 with an empty body
  WHEN Client.Render is called
  THEN it MUST return an error indicating the response body is empty

---

### T2: Snapshot Result Types
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Define the `SnapshotResult` struct in `internal/dashboards/types.go` used for both JSON output (agent mode) and table output (human mode). Fields: `UID`, `PanelID`, `FilePath`, `Width`, `Height`, `Theme`, `RenderedAt`, `FileSize`.

**Deliverables:**
- `internal/dashboards/types.go`

**Acceptance criteria:**
- GIVEN a SnapshotResult is marshalled to JSON
  WHEN panel_id is zero
  THEN the `panel_id` field MUST be serialized as `null` (using `*int` or `omitempty` as appropriate)

- GIVEN a SnapshotResult is marshalled to JSON
  WHEN all fields are populated
  THEN the output MUST contain `uid`, `panel_id`, `file_path`, `width`, `height`, `theme`, and `rendered_at` fields

---

## Wave 2: CLI Command

### T3: Snapshot Command Implementation
**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T1, T2
**Type**: task

Implement `cmd/gcx/dashboards/command.go` (command group) and `cmd/gcx/dashboards/snapshot.go` (snapshot subcommand). Follow the Options pattern: `snapshotOpts` struct with `setup(flags)`, `Validate()`, and `snapshotCmd(configOpts)` constructor.

The command:
1. Validates at least one UID is provided (cobra `Args: cobra.MinimumNArgs(1)`)
2. Validates `--window` mutual exclusivity with `--from`/`--to`
3. Creates output directory via `os.MkdirAll`
4. Constructs the render client from the active config context
5. Renders all UIDs concurrently via errgroup with bounded parallelism
6. Writes PNG files with deterministic naming (`{uid}.png` / `{uid}-panel-{panelId}.png`)
7. Logs debug message when overwriting existing files
8. Outputs JSON array in agent mode, table in human mode

Implement `cmd/gcx/dashboards/snapshot_test.go` with table-driven tests for `Validate()` logic (window exclusivity, missing UID, default dimensions).

**Deliverables:**
- `cmd/gcx/dashboards/command.go`
- `cmd/gcx/dashboards/snapshot.go`
- `cmd/gcx/dashboards/snapshot_test.go`

**Acceptance criteria:**
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

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --window 6h`
  THEN the render request MUST include query parameters `from=now-6h` and `to=now`

- GIVEN a configured gcx context
  WHEN the user runs `gcx dashboards snapshot <uid> --window 6h --from now-2h`
  THEN the command MUST exit with a non-zero code and print a validation error indicating that `--window` is mutually exclusive with `--from`/`--to`

- GIVEN no dashboard UID is provided
  WHEN the user runs `gcx dashboards snapshot`
  THEN the command MUST exit with a non-zero code and print an error message indicating that at least one dashboard UID is required

- GIVEN agent mode is active
  WHEN the user runs `gcx dashboards snapshot <uid1> <uid2>`
  THEN stdout MUST contain a JSON array with two objects, each containing `uid`, `file_path` (absolute path), `width`, `height`, `theme`, and `rendered_at` fields

- GIVEN the user provides multiple dashboard UIDs
  WHEN the user runs `gcx dashboards snapshot <uid1> <uid2> <uid3>`
  THEN all three dashboards MUST be rendered concurrently and three PNG files MUST be written

- GIVEN the Grafana instance does not have Image Renderer installed
  WHEN the user runs `gcx dashboards snapshot <uid>`
  THEN the command MUST exit with a non-zero code and report the HTTP error from Grafana

- GIVEN a valid render request
  WHEN the Grafana API returns an HTTP response with a non-200 status code
  THEN the command MUST exit with a non-zero code and include the HTTP status code and response body excerpt in the error message

---

## Wave 3: Integration and Documentation

### T4: Root Command Registration and Integration Tests
**Priority**: P0
**Effort**: Small
**Depends on**: T3
**Type**: task

Register the `dashboards.Command()` in `cmd/gcx/root/command.go` by adding the import and `rootCmd.AddCommand(dashboards.Command())` call. Run `make all` (with `GCX_AGENT_MODE=false`) to verify the build succeeds, docs regenerate correctly, and no existing tests break.

**Deliverables:**
- `cmd/gcx/root/command.go` (modified — add import + AddCommand)

**Acceptance criteria:**
- GIVEN the gcx binary is built
  WHEN the user runs `gcx dashboards --help`
  THEN the help text MUST list `snapshot` as an available subcommand

- GIVEN the gcx binary is built
  WHEN the user runs `gcx dashboards snapshot --help`
  THEN the help text MUST list all flags: `--width`, `--height`, `--theme`, `--from`, `--to`, `--window`, `--tz`, `--org-id`, `--panel`, `--output-dir`, `--concurrency`

- GIVEN `GCX_AGENT_MODE=false make all` is run
  THEN the build MUST succeed, linting MUST pass, tests MUST pass, and docs MUST regenerate without drift

---

### T5: Skill Documentation Updates
**Priority**: P1
**Effort**: Small
**Depends on**: T3
**Type**: chore

Update `claude-plugin/skills/manage-dashboards/SKILL.md` with a new "Workflow 6: Capture Dashboard Snapshots" section documenting the `gcx dashboards snapshot` command with example commands for full dashboard and single panel rendering.

Update `claude-plugin/skills/debug-with-grafana/SKILL.md` to add a snapshot step within "Step 6: Check Related Dashboards and Resources" that references the `gcx dashboards snapshot` command as a way to visually inspect dashboard state during debugging.

**Deliverables:**
- `claude-plugin/skills/manage-dashboards/SKILL.md` (modified)
- `claude-plugin/skills/debug-with-grafana/SKILL.md` (modified)

**Acceptance criteria:**
- GIVEN the `manage-dashboards` skill file exists
  WHEN an agent reads `claude-plugin/skills/manage-dashboards/SKILL.md`
  THEN it MUST contain a workflow section for capturing dashboard snapshots with example commands

- GIVEN the `debug-with-grafana` skill file exists
  WHEN an agent reads `claude-plugin/skills/debug-with-grafana/SKILL.md`
  THEN it MUST reference the `gcx dashboards snapshot` command as a diagnostic tool
