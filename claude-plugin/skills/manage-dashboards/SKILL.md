---
name: manage-dashboards
description: >
  Use this skill when the user wants to pull dashboards from Grafana to local
  files, push local dashboard files to Grafana, create a new dashboard from
  scratch, validate dashboard files against the Grafana API schema, promote
  dashboards across environments (dev, staging, production), manage Grafana
  folders and their associated dashboards, or capture visual snapshots
  (screenshots/PNG images) of dashboards or individual panels. Also use this
  skill when the user asks about editing, deleting, or diffing dashboards,
  about the live development server for iterative dashboard authoring, or
  wants to render/export a dashboard image.
---

# Manage Dashboards

This skill guides agents through the full dashboard lifecycle using gcx:
pulling from Grafana, pushing to Grafana, creating new dashboards, validating
files, and promoting dashboards across environments. All operations work with
Kubernetes-style resource files on disk.

## Prerequisites

gcx must be installed and configured with a working context pointing to
your Grafana instance. If gcx is not configured, use the
`setup-gcx` skill first.

```bash
# Verify configuration and connectivity
gcx config check
```

---

## Workflow 1: Pull Dashboards from Grafana

Pull downloads resources from Grafana and writes them as local JSON or YAML
files. Use pull to create a local working copy, back up dashboards, or
bootstrap a GitOps repository.

### Pull all dashboards

```bash
# Pull all dashboards into ./resources (JSON, default)
gcx resources pull dashboards

# Pull as YAML
gcx resources pull dashboards -o yaml

# Pull to a custom directory
gcx resources pull dashboards -p ./my-dashboards
```

### Pull folders and dashboards together

Pulling folders alongside dashboards preserves the folder hierarchy on disk.
Always pull both when you intend to push them to another environment later.

```bash
gcx resources pull dashboards folders
gcx resources pull dashboards folders -p ./resources -o yaml
```

### Pull a specific dashboard

```bash
# Pull by UID
gcx resources pull dashboards/my-dashboard-uid

# Pull multiple specific dashboards
gcx resources pull dashboards/uid-a dashboards/uid-b

# Pull dashboards matching a glob pattern
gcx resources pull dashboards/prod-*
```

### Pull dashboards managed by other tools

By default, pull only fetches dashboards managed by gcx. To include
dashboards created via the UI, Terraform, or other tools:

```bash
gcx resources pull dashboards --include-managed
```

### Inspect pulled resources

After pulling, files appear in the target directory structured by kind:

```
./resources/
  dashboards/
    my-dashboard.json
    another-dashboard.yaml
  folders/
    my-folder.json
```

Use `gcx resources get` to inspect resources without writing files:

```bash
gcx resources get dashboards -o json
gcx resources get dashboards/my-uid -o yaml
```

---

## Workflow 2: Push Dashboards to Grafana

Push reads local resource files and writes them to Grafana. gcx handles
folder ordering automatically and protects resources managed by other tools.

### Topological sort: folders are pushed before dashboards

When pushing a directory that contains both folders and dashboards, gcx
automatically pushes folders first (level by level) before pushing dashboards.
Dashboards reference their parent folder via `spec.folderUID`; the folder must
exist before the dashboard can be created or updated. **You do not need to
split the push into two separate commands** — gcx's topological sort
handles ordering automatically.

```bash
# Push everything under ./resources — folders created before dashboards
gcx resources push

# Push from a specific directory
gcx resources push -p ./my-resources

# Explicitly include both kinds (still sorted automatically)
gcx resources push dashboards folders
```

### Manager metadata

When gcx pushes a resource, it sets this annotation automatically:

```yaml
metadata:
  annotations:
    grafana.app/managed-by: gcx
```

Resources that carry a **different** `grafana.app/managed-by` value (set by
the Grafana UI, Terraform, or another tool) are **protected by default**.
gcx refuses to overwrite them unless you pass `--include-managed`.

```bash
# Override protection — only when you deliberately want to take ownership
gcx resources push --include-managed
```

### Dry run before pushing to production

Always dry-run before pushing to production environments to preview what will
change:

```bash
gcx resources push --dry-run
gcx resources push -p ./my-resources --dry-run
```

### Push specific kinds or UIDs

```bash
# Push only dashboards (folders must already exist in Grafana)
gcx resources push dashboards

# Push a single dashboard file
gcx resources push -p ./resources/dashboards/my-dashboard.json
```

### Error handling during push

```bash
# Stop on first error (useful when later resources depend on earlier ones)
gcx resources push --on-error abort

# Continue past errors, report all failures at the end (default)
gcx resources push --on-error fail

# Ignore per-resource errors entirely (CI pipelines with partial success)
gcx resources push --on-error ignore
```

---

## Workflow 3: Create a New Dashboard

Creating a new dashboard with gcx involves authoring a resource file
locally and then pushing it to Grafana.

### Step 1: Get an existing dashboard as a template

Pull a similar dashboard to use as a starting point:

```bash
gcx resources pull dashboards/existing-uid -p ./templates -o yaml
```

Or list available dashboards to find a suitable one:

```bash
gcx resources get dashboards -o wide
```

### Step 2: Author the resource file

Create a new YAML or JSON file. Set `metadata.name` to a human-readable name;
leave `metadata.uid` empty (gcx assigns a UID on first push) or set it
explicitly to a value you choose.

Minimal dashboard resource structure:

```yaml
apiVersion: dashboard.grafana.app/v1alpha1
kind: Dashboard
metadata:
  name: my-new-dashboard
  # uid: leave empty for auto-assignment, or set explicitly
spec:
  title: "My New Dashboard"
  tags: []
  panels: []
  # Optional: place in a folder
  # folderUID: <folder-uid>
```

### Step 3: Validate before pushing

```bash
# Validate the file against Grafana's API schema
gcx resources validate -p ./my-new-dashboard.yaml
```

Resolve any validation errors before proceeding.

### Step 4: Push the new dashboard

```bash
gcx resources push -p ./my-new-dashboard.yaml
```

gcx assigns a UID and sets `grafana.app/managed-by: gcx`
automatically. Pull the dashboard after pushing to capture the assigned UID:

```bash
gcx resources pull dashboards/<assigned-uid>
```

### Step 5: Iterate with the live dev server

For iterative panel authoring, use `serve` to get instant browser previews on
every file save:

```bash
gcx dev serve ./dashboards
```

See the `serve` command reference in
[`references/resource-operations.md`](references/resource-operations.md) for
full flag details and the hot-reload workflow.

---

## Workflow 4: Validate Dashboard Files

Validate checks local resource files against the Grafana API schema without
writing anything to Grafana. Use this in CI/CD pipelines and before pushing to
production.

### Validate default directory

```bash
gcx resources validate
```

### Validate a specific path

```bash
gcx resources validate -p ./dashboards
gcx resources validate -p ./resources/dashboards/my-dashboard.yaml
```

### Validate multiple directories

```bash
gcx resources validate -p ./dashboards -p ./folders
```

### Validate and output as JSON (CI/CD)

```bash
gcx resources validate -o json
```

Expected output structure (field names, not fabricated values):

```json
{
  "results": [
    {
      "file": "<path>",
      "kind": "Dashboard",
      "name": "<name>",
      "uid": "<uid>",
      "valid": true,
      "errors": []
    }
  ],
  "summary": {
    "total": "<count>",
    "valid": "<count>",
    "invalid": "<count>"
  }
}
```

A non-zero exit code indicates at least one resource failed validation.

---

## Workflow 5: Promote Dashboards Across Environments

Promoting dashboards means pulling them from a source environment (e.g.,
staging) and pushing them to a target environment (e.g., production). This
uses gcx's multi-context support.

### Prerequisites: one context per environment

Each environment needs a named context in your gcx configuration. If
you have not set up multi-context configuration, use the `setup-gcx`
skill to create contexts for staging and production.

```bash
# Verify your contexts
gcx config view

# Example: switch active context
gcx config use-context staging
gcx config use-context production
```

### Option A: use --context flag (no active-context switch)

The `--context` flag targets a specific context for a single command without
changing the active context globally. This is the safest pattern for promotion
scripts.

```bash
# Step 1: Pull dashboards from staging
gcx resources pull --context staging dashboards folders -p ./promote

# Step 2: Review what was pulled
gcx resources validate -p ./promote

# Step 3: Dry-run push to production
gcx resources push --context production -p ./promote --dry-run

# Step 4: Push to production
gcx resources push --context production -p ./promote
```

### Option B: switch active context with use-context

```bash
# Pull from staging
gcx config use-context staging
gcx resources pull dashboards folders -p ./promote

# Push to production
gcx config use-context production
gcx resources push -p ./promote
```

### Folder ordering during promotion

Because the promote directory contains both folders and dashboards, gcx
automatically pushes folders before dashboards in the target environment. You
do not need to run separate commands for folders and dashboards.

### Handling manager metadata during promotion

Dashboards pulled from staging carry `grafana.app/managed-by: gcx`.
When you push them to production, gcx recognizes the annotation and
allows the push without `--include-managed`. If the production environment
already has dashboards managed by another tool (UI, Terraform), add
`--include-managed` to take ownership:

```bash
gcx resources push --context production -p ./promote --include-managed
```

### Full promotion script pattern

```bash
#!/bin/bash
set -e
SOURCE_CTX=staging
TARGET_CTX=production
WORK_DIR=$(mktemp -d)

# Pull from source
gcx resources pull --context "$SOURCE_CTX" dashboards folders -p "$WORK_DIR"

# Validate
gcx resources validate -p "$WORK_DIR"

# Dry run on target
gcx resources push --context "$TARGET_CTX" -p "$WORK_DIR" --dry-run

# Apply
gcx resources push --context "$TARGET_CTX" -p "$WORK_DIR"
```

---

## Workflow 6: Capture Dashboard Snapshots

Render a Grafana dashboard or individual panel to a PNG image using the Grafana
Image Renderer. Requires the `grafana-image-renderer` plugin on the Grafana
instance.

> **If stuck**, run `gcx dashboards snapshot --help` for the full flag
> reference, or `gcx dashboards --help` to see available subcommands.

### Step 1: Find the dashboard UID

```bash
# List all dashboards to find UIDs
gcx resources get dashboards

# Get a specific dashboard by name substring (use -ojson for programmatic access)
gcx resources get dashboards -ojson | jq '.items[] | {uid: .metadata.name, title: .spec.title}'
```

### Step 2: Discover template variables (if the dashboard uses them)

Most dashboards have template variables (cluster, datasource, job, etc.) that
control what data is displayed. To render a meaningful snapshot, you should set
these to the values relevant to the user's context.

```bash
# Inspect the dashboard's template variables
gcx resources get dashboards/<uid> -ojson | jq '.spec.templating.list[] | {name, type, current: .current.value}'
```

This shows each variable's name and its current default value. Use `--var` to
override any of these during rendering.

### Step 3: Render the snapshot

```bash
# Basic: full dashboard, current directory
gcx dashboards snapshot <uid>

# With output directory
gcx dashboards snapshot <uid> --output-dir ./snapshots

# With template variable overrides (match the dashboard's variable names)
gcx dashboards snapshot <uid> --var cluster=prod --var datasource=grafanacloud-prom

# With time range
gcx dashboards snapshot <uid> --since 6h --var cluster=prod
gcx dashboards snapshot <uid> --from now-1h --to now --tz UTC

# Single panel (find panel IDs from the dashboard JSON: .spec.panels[].id)
gcx dashboards snapshot <uid> --panel 42

# Custom dimensions and theme
gcx dashboards snapshot <uid> --width 1280 --height 720 --theme light

# Multiple dashboards concurrently
gcx dashboards snapshot uid-a uid-b uid-c --output-dir ./snapshots
```

### Output

**Agent mode** (auto-detected): JSON array to stdout with file paths and metadata:

```json
[{"uid": "<uid>", "panel_id": null, "file_path": "/abs/path/<uid>.png", "width": 1920, "height": -1, "theme": "dark", "rendered_at": "<RFC3339>"}]
```

**Human mode**: table with columns UID, Panel, File, Size.

Files are named `{uid}.png` (full dashboard) or `{uid}-panel-{panelId}.png` (single panel).

### Troubleshooting

```bash
# If rendering fails with 500 or "plugin not found":
# → The grafana-image-renderer plugin is likely not installed on the Grafana instance

# If the snapshot shows default/wrong variable values:
# → Inspect variables and pass the right ones with --var
gcx resources get dashboards/<uid> -ojson | jq '.spec.templating.list[] | {name, current: .current.value}'

# If the snapshot is cut off or too small:
# → Default height is -1 (full page). Override with --height if needed.
# → For panels, default is 800x600. Override with --width/--height.

# Full flag reference:
gcx dashboards snapshot --help
```

---

## Common Operations

### Delete a dashboard

```bash
# Delete a specific dashboard
gcx resources delete dashboards/my-uid

# Dry-run before bulk delete
gcx resources delete dashboards/temp-* --dry-run
gcx resources delete dashboards/temp-* -y
```

### Edit a dashboard in-place

Opens the resource in `$EDITOR`, then pushes the updated version:

```bash
gcx resources edit dashboards/my-uid
gcx resources edit dashboards/my-uid -o yaml
```

### List available resource kinds

```bash
gcx resources schemas
```

---

## References

- [`references/resource-operations.md`](references/resource-operations.md) —
  Full flag reference for all `gcx resources` subcommands, selector
  syntax, and `serve` workflow details.

- [`references/resource-model.md`](references/resource-model.md) —
  Kubernetes-style resource structure, manager metadata behavior, dependency
  rules (folders before dashboards), push ordering phases, and resource
  lifecycle (create, read, update, delete).
