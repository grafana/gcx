# Resource Operations Reference

<!-- Flag names and defaults verified against gcx source as of T1 audit (2026-03-07).
     Source files: cmd/gcx/resources/get.go, push.go, pull.go, delete.go, edit.go,
     validate.go, serve.go. Do not update examples without re-auditing the CLI. -->

This file documents the full flag set and usage patterns for each `gcx resources`
subcommand, the selector syntax, and the `serve` live development workflow.

---

## Selector Syntax

Selectors are positional arguments passed to commands that accept `[RESOURCE_SELECTOR]...`.
They control which resources the command operates on.

### Kind Selector

Targets all resources of a given kind. The kind name is the plural resource name as
reported by `gcx resources schemas`.

```bash
gcx resources get dashboards
gcx resources pull dashboards
gcx resources delete dashboards --force
```

### UID Selector

Targets a single resource by its UID. Format: `<kind>/<uid>`.

```bash
gcx resources get dashboards/my-dashboard-uid
gcx resources edit dashboards/my-dashboard-uid
gcx resources delete dashboards/my-dashboard-uid
```

### Glob Pattern

Targets resources whose UID matches a glob pattern. Use `*` for any substring within a
UID segment.

```bash
gcx resources get dashboards/my-*
gcx resources pull dashboards/prod-*
gcx resources delete dashboards/temp-* --dry-run
```

### Multi-Selector

Pass multiple selectors as separate positional arguments to target more than one kind or
UID set in a single command. Selectors are evaluated independently and their results are
merged.

```bash
# All dashboards and all folders
gcx resources get dashboards folders

# Two specific dashboards plus all folders
gcx resources get dashboards/uid-a dashboards/uid-b folders
```

---

## `gcx resources get`

Fetch and display resources from Grafana.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--output` | `-o` | `text` | `text`, `wide`, `json`, `yaml` | Output format |
| `--on-error` | | `fail` | `ignore`, `fail`, `abort` | Error handling policy (see below) |

### Usage

```bash
# List all dashboards as a table
gcx resources get dashboards

# Get a specific dashboard in YAML
gcx resources get dashboards/my-uid -o yaml

# Get all folders in JSON (useful for scripting)
gcx resources get folders -o json

# Get dashboards and folders together, wide output
gcx resources get dashboards folders -o wide

# Get dashboards matching a glob, ignore per-resource errors
gcx resources get dashboards/prod-* --on-error ignore
```

---

## `gcx resources push`

Write local resource files to Grafana. Handles folder/dashboard topological ordering
automatically: folders are pushed level-by-level before dashboards when both are present.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--path` | `-p` | `./resources` | path (repeatable) | Directories or files to read from |
| `--max-concurrent` | | `10` | integer | Maximum parallel push operations |
| `--on-error` | | `fail` | `ignore`, `fail`, `abort` | Error handling policy |
| `--dry-run` | | `false` | bool | Validate and plan without writing |
| `--omit-manager-fields` | | `false` | bool | Strip `grafana.app/managed-by` annotation before push |
| `--include-managed` | | `false` | bool | Push resources managed by other tools (overrides protection) |

### Usage

```bash
# Push everything under ./resources (default path)
gcx resources push

# Push from a specific directory
gcx resources push -p ./dashboards

# Push from multiple directories
gcx resources push -p ./dashboards -p ./folders

# Dry run: show what would change without writing
gcx resources push --dry-run

# Push and override resources managed by other tools
gcx resources push --include-managed

# Push with abort-on-first-error and limited concurrency
gcx resources push --on-error abort --max-concurrent 5
```

### Manager Metadata Behavior

When gcx pushes a resource it sets the annotation:

```yaml
annotations:
  grafana.app/managed-by: gcx
```

Resources that carry a different `grafana.app/managed-by` value (set by Terraform, the
Grafana UI, or another tool) are **protected by default**. gcx will refuse to push
over them unless `--include-managed` is passed. This prevents accidental overwrites of
resources managed by other systems.

---

## `gcx resources pull`

Download resources from Grafana and write them to local files.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--output` | `-o` | `json` | `json`, `yaml` | On-disk file format for written resources |
| `--on-error` | | `fail` | `ignore`, `fail`, `abort` | Error handling policy |
| `--path` | `-p` | `./resources` | path | Directory to write resource files into |
| `--include-managed` | | `false` | bool | Also pull resources managed by other tools |

### Usage

```bash
# Pull all dashboards into ./resources (JSON format)
gcx resources pull dashboards

# Pull to a custom directory in YAML format
gcx resources pull dashboards -p ./my-dashboards -o yaml

# Pull dashboards and folders together
gcx resources pull dashboards folders

# Pull a specific dashboard
gcx resources pull dashboards/my-uid

# Pull resources regardless of who manages them
gcx resources pull dashboards --include-managed
```

---

## `gcx resources delete`

Delete resources from Grafana.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--on-error` | | `fail` | `ignore`, `fail`, `abort` | Error handling policy |
| `--max-concurrent` | | `10` | integer | Maximum parallel delete operations |
| `--force` | | `false` | bool | Required when using kind-only selectors (deletes all of that kind) |
| `--dry-run` | | `false` | bool | Show what would be deleted without deleting |
| `--path` | `-p` | none | path (repeatable) | Delete resources listed in files at this path |
| `--yes` | `-y` | `false` | bool | Skip confirmation prompt for destructive operations |

### Usage

```bash
# Delete a specific dashboard (no --force needed for UID selectors)
gcx resources delete dashboards/my-uid

# Delete all dashboards matching a glob — dry run first
gcx resources delete dashboards/temp-* --dry-run
gcx resources delete dashboards/temp-* -y

# Delete all dashboards of a kind (requires --force as a safety gate)
gcx resources delete dashboards --force --dry-run
gcx resources delete dashboards --force -y

# Delete resources from local files
gcx resources delete -p ./old-dashboards -y
```

> **Safety**: Kind-only selectors require `--force` to prevent accidental mass-deletion.
> Always use `--dry-run` first when deleting by glob or kind.

---

## `gcx resources edit`

Open a single resource in `$EDITOR` for in-place editing, then push the updated resource
back to Grafana.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--output` | `-o` | `json` | `json`, `yaml` | Format used in the editor buffer |

### Usage

```bash
# Edit a dashboard in the default format (JSON)
gcx resources edit dashboards/my-uid

# Edit using YAML in the editor
gcx resources edit dashboards/my-uid -o yaml
```

Edit accepts exactly one positional selector. The resource is fetched, opened in
`$EDITOR`, and pushed back on save. Exiting without changes is a no-op.

---

## `gcx resources validate`

Validate local resource files against the Grafana API schema without writing anything.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--output` | `-o` | `text` | `text`, `json`, `yaml` | Output format for validation results |
| `--path` | `-p` | `./resources` | path (repeatable) | Directories or files to validate |
| `--max-concurrent` | | `10` | integer | Maximum parallel validation operations |
| `--on-error` | | `fail` | `ignore`, `fail`, `abort` | Error handling policy |

### Usage

```bash
# Validate everything under ./resources
gcx resources validate

# Validate a specific directory
gcx resources validate -p ./dashboards

# Validate and output results as JSON (useful in CI)
gcx resources validate -o json

# Validate from multiple directories
gcx resources validate -p ./dashboards -p ./folders
```

---

## `gcx dev serve`

Start a live development server that watches local resource files and hot-reloads them
into Grafana as they change. The browser refreshes automatically when a resource is
updated.

### Flags

| Flag | Short | Default | Values | Description |
|------|-------|---------|--------|-------------|
| `--address` | | `0.0.0.0` | host | Address to bind the dev server to |
| `--port` | | `8080` | integer | Port to bind the dev server to |
| `--watch` | `-w` | none | path (repeatable) | Additional paths to watch for changes |
| `--no-watch` | | `false` | bool | Disable file watching (serve once and exit) |
| `--script` | `-S` | none | path | Script to run to generate the resource |
| `--script-format` | `-f` | `json` | `json`, `yaml` | Output format expected from the script |
| `--max-concurrent` | | `10` | integer | Maximum parallel push operations on reload |

Positional arguments are resource directories to serve.

### Live Dev Server Workflow

The `serve` command creates a tight edit-preview loop for dashboard development:

1. **Start the server** — gcx watches the resource directories and listens for
   browser connections.
2. **Edit a resource file** — change the JSON/YAML on disk (manually or via a script).
3. **Hot reload** — gcx detects the file change, pushes the updated resource to
   Grafana, and signals connected browsers to refresh the dashboard panel view.
4. **Browser preview** — open `http://localhost:8080` (or the configured address/port)
   to see the live Grafana panel. The preview refreshes automatically on each save.

```
Developer          gcx serve       Grafana API        Browser
    |                     |                    |                 |
    |-- edit file ------->|                    |                 |
    |                     |-- push resource -->|                 |
    |                     |<-- 200 OK ---------                 |
    |                     |-- ws: reload signal --------------->|
    |                     |                    |<-- GET panel ---|
```

### Usage

```bash
# Serve resources from the default directory (./resources)
gcx dev serve .

# Serve a specific directory
gcx dev serve ./dashboards

# Serve with a custom port, watch additional path
gcx dev serve ./dashboards --port 9090 -w ./shared-panels

# Serve a script-generated resource (re-runs script on each file change)
gcx dev serve . --script ./generate-dashboard.sh --script-format json

# Serve without watching (push once and exit)
gcx dev serve ./dashboards --no-watch
```

---

## `--on-error` Policy Reference

All commands that process multiple resources accept `--on-error` to control behavior when
an individual resource operation fails.

| Value | Behavior | Exit Code |
|-------|----------|-----------|
| `fail` | Continue processing remaining resources; exit non-zero if any failed | 1 if any failed |
| `ignore` | Continue processing remaining resources; always exit zero | 0 |
| `abort` | Stop immediately on first failure; exit non-zero | 1 |

**Default**: `fail` — processes all resources and reports failures at the end without
stopping the batch.

Use `ignore` in CI pipelines where partial success is acceptable. Use `abort` when
subsequent operations depend on all prior operations succeeding (e.g., folder creation
before dashboard push in a multi-step script).
