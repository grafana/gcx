---
name: manage-dashboards
description: >
  Use for operational management of existing Grafana dashboards: list, get,
  search, create or update from an already-authored manifest, delete, inspect
  and restore versions, pull/push/validate/promote dashboard resource files,
  manage dashboard folders, or render PNG snapshots. Do NOT use when the task
  involves adding new panels, variables, or annotations — those require
  discovering real metrics or log schema, so use create-dashboard instead.
  For designing or creating a new dashboard, or for material visual/dashboard
  UX changes, also use create-dashboard.
---

# Manage Dashboards

Use `gcx` dedicated commands first. Only use `gcx api` when a dedicated command
cannot perform the requested operation.

## Routing

| User intent | Use |
|-------------|-----|
| Build a new dashboard from an idea, service, SLO, incident, or set of metrics | `create-dashboard` |
| Redesign layout, choose panels/queries, or visually iterate dashboard quality | `create-dashboard` |
| Generate a Go builder skeleton only | `generate-resource-stubs` |
| Convert an existing live dashboard to Go code | `import-dashboards` |
| List, search, inspect, delete, restore, pull, push, validate, promote, or snapshot existing dashboards | this skill |
| Configure gcx/auth first | `setup-gcx` |

## Preflight for Mutations

Before any create/update/delete/push/restore operation:

```bash
gcx config current-context
gcx config check
```

Use `--context <name>` when the user named a target environment. Do not switch
the global context unless the user asked for it.

For writes, read current state first and preserve folder/manager intent:

```bash
gcx dashboards get <dashboard-name> -o json
gcx resources get folders -o json
```

Manager boundary: gcx protects resources managed by another tool. If a push
fails because of `grafana.app/managed-by`, stop and ask/confirm before using
`--include-managed`.

## Typical Update Workflow

1. Preflight — `gcx config check`, then fetch current state with `gcx dashboards get <name> -o json`
2. Edit — write the modified YAML locally
3. Validate and push — `gcx resources validate -p <path> -o json`, `gcx resources push -p <path> --dry-run`, `gcx resources push -p <path>`
4. Verify — `gcx dashboards get <name> -o json`

## Operation Reference

Use JSON/YAML for programmatic work and table/wide output for human summaries.

| Operation | Command pattern |
|-----------|-----------------|
| List datasources | `gcx datasources list -o json` |
| Prometheus metric names | `gcx datasources prometheus labels -d <uid> --label __name__` |
| Prometheus label values | `gcx datasources prometheus labels -d <uid> --label <label>` |
| Prometheus metric metadata | `gcx datasources prometheus metadata -d <uid>` |
| Loki label names | `gcx datasources loki labels -d <uid>` |
| Loki label values | `gcx datasources loki labels -d <uid> --label <label>` |
| Tempo attribute names | `gcx datasources tempo labels -d <uid>` |
| Tempo attribute values | `gcx datasources tempo labels -d <uid> -l service.name` |
| Pyroscope profile types | `gcx datasources pyroscope profile-types -d <uid>` |
| Pyroscope label values | `gcx datasources pyroscope labels -d <uid> --label service_name` |
| Other datasource types | Check `gcx datasources <type> --help` for dedicated subcommands before using `gcx api` |
| List dashboards | `gcx dashboards list -o wide` |
| Search by text/tag/folder | `gcx dashboards search "<query>" --tag <tag> --folder <folder-name> -o json` |
| Get one dashboard | `gcx dashboards get <dashboard-name> -o json` |
| Create from finished file | `gcx dashboards create -f <dashboard.yaml>` |
| Update from finished file | `gcx dashboards update <dashboard-name> -f <dashboard.yaml>` |
| Delete with confirmation | `gcx dashboards delete <dashboard-name>` |
| Delete non-interactively | `gcx dashboards delete <dashboard-name> --yes` |
| Version history | `gcx dashboards versions list <dashboard-name>` |
| Restore version | `gcx dashboards versions restore <dashboard-name> <version> --message "<why>"` |
| Pull dashboards/folders | `gcx resources pull dashboards folders -p <dir> -o yaml` |
| Pull one dashboard | `gcx resources pull dashboards/<dashboard-name> -p <dir> -o yaml` |
| Validate local files | `gcx resources validate -p <path> -o json` |
| Preview push | `gcx resources push -p <path> --dry-run` |
| Push local files | `gcx resources push -p <path>` |
| Delete by selector | `gcx resources delete dashboards/<dashboard-name>` |
| Edit in `$EDITOR` | `gcx resources edit dashboards/<dashboard-name> -o yaml` |
| List resource kinds | `gcx resources schemas` |

`<dashboard-name>` is the dashboard resource name (`metadata.name`), which is
also the value accepted by `gcx dashboards snapshot`.

## Pull/Push Notes

- Pull output directories may include API version/group in their path. Use the
  paths printed by gcx; do not assume a fixed `dashboards/` directory shape.
- When a directory contains folders and dashboards, gcx pushes folders first.
- Use `--on-error abort` when later resources depend on earlier ones and partial
  progress would be confusing.
- Dry-run before writing to production unless the user explicitly opts out.

## Snapshots

```bash
GCX_AGENT_MODE=true gcx dashboards snapshot <dashboard-name> --output-dir ./snapshots --since 6h
# With variables:
GCX_AGENT_MODE=true gcx dashboards snapshot <dashboard-name> --output-dir ./snapshots --since 6h --var cluster=prod
```

Read the returned `file_path` PNG and summarize what you see; do not just report the path.

Troubleshooting: `plugin not found`/renderer 500 = image renderer unavailable; wrong data = inspect
template variables and rerun with `--var`; auth errors = check `gcx config check`.

## Version Restore Safety

Restore performs a read of historical content and writes a new current revision.
Always include a message and verify afterwards:

```bash
gcx dashboards versions list <dashboard-name> --limit 10
gcx dashboards versions restore <dashboard-name> <version> \
  --message "Restore known-good dashboard after <reason>"
gcx dashboards get <dashboard-name> -o json
```

A conflict or 409 means someone changed the dashboard concurrently. Re-fetch,
review the newest version, and retry only if the restore is still correct.

## Common Failure Handling

| Symptom | Action |
|---------|--------|
| `gcx config check` fails | Use `setup-gcx` before dashboard operations |
| `gcx resources get datasources` selector error | Use `gcx datasources list` instead — `datasource.grafana.app` is not registered on all instances |
| Dashboard not found | Search first; confirm `metadata.name`, not just title |
| Folder filter does not work with list | Use `gcx dashboards search --folder <folder-name>` |
| Push blocked by manager metadata | Ask before `--include-managed` |
| Validation fails | Fix local file; do not push invalid resources |
| Snapshot returns wrong variables | Inspect templating and pass `--var name=value` |
| Snapshot unavailable | Report renderer/auth blocker; do not claim visual review |

## References

- [`references/resource-operations.md`](references/resource-operations.md) —
  selector syntax, pull/push/validate flags, and `gcx dev serve` details.
- [`references/resource-model.md`](references/resource-model.md) — resource
  structure, manager metadata, folder ordering, and lifecycle behavior.
