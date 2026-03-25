# ADR-001: Multi-File Config Layering (System/User/Local)

**Created**: 2026-03-21
**Status**: accepted
**Bead**: none
**Supersedes**: none

## Context

grafanactl loads config from a single file — the first file found on the XDG search path. There is no way to:

- Apply system-wide defaults (e.g., a shared Grafana server URL for a team)
- Override specific fields per project without modifying the user config
- See which config file is actually loaded
- Open a config file in an editor without knowing its path

This becomes a pain point as users work across multiple Grafana stacks and projects. The `--config` flag is a workaround but requires remembering the path and can't be automated.

## Decision

### Config Layering

The loader discovers and deep-merges configs from up to three file sources, then applies env var / flag overrides on top:

```
┌─────────────────┐  priority 3 (lowest)
│ System           │  $XDG_CONFIG_DIRS/grafanactl/config.yaml
└────────┬────────┘  (Linux: /etc/xdg/, macOS: /Library/Application Support/)
         ▼ deep merge
┌─────────────────┐  priority 2
│ User             │  $XDG_CONFIG_HOME/grafanactl/config.yaml
└────────┬────────┘  (default: ~/.config/)
         ▼ deep merge
┌─────────────────┐  priority 1 (highest)
│ Local            │  .grafanactl.yaml (in working directory)
└────────┬────────┘
         ▼ override functions
┌─────────────────┐
│ Env vars/flags   │  GRAFANA_*, --config, --context
└─────────────────┘

--config flag: bypasses all layering, loads only that file.
```

**Deep merge rules:**
- Scalar fields (`current-context`): higher priority wins if non-empty; zero values in higher layers do not erase lower layer values
- Maps (`contexts`, `providers`, `datasources`): merge by key — same key does a field-level merge, new keys are added
- Struct fields within a context: higher priority wins per-field (e.g., user layer can set `grafana.token`, local layer can add `cloud.token` without erasing the token)

**Source tracking:** The merged `Config` gains a `Sources []ConfigSource` field (not serialized) that records which files contributed:

```go
type ConfigSource struct {
    Path    string    // absolute file path
    Type    string    // "system", "user", "local", "explicit"
    ModTime time.Time // for display
}
```

Files that don't exist are silently skipped. If zero files exist, the user-level config is auto-created (preserving current behavior).

### `config path` Command

Shows loaded config files with type, priority, and modification time:

```
$ grafanactl config path
PATH                                                 TYPE    PRIORITY     MODIFIED
/Users/igor/Code/myproject/.grafanactl.yaml          local   1 (highest)  2026-03-21 09:10:05
/Users/igor/.config/grafanactl/config.yaml           user    2            2026-03-21 08:45:12
/Library/Application Support/grafanactl/config.yaml  system  3 (lowest)   2026-03-15 10:22:31
```

Respects `--output` flag (table/json/yaml). Shows only files that exist.

### `config edit` Command

Opens a config file in `$EDITOR` (falls back to `vi` / `notepad`):

```
$ grafanactl config edit              # opens if only one config exists; errors if multiple
$ grafanactl config edit user         # opens user config by type
$ grafanactl config edit local --create  # creates .grafanactl.yaml if missing, then opens it
```

### `--file` Flag on `config set` / `config unset`

When multiple configs are loaded, `config set` requires `--file <type>` to target a specific layer:

```bash
grafanactl config set --file local contexts.prod.cloud.token my-token
grafanactl config set --file user  contexts.prod.grafana.server https://prod.grafana.net
```

Without `--file` and multiple configs present: error with suggestion to add `--file`. With a single config: behaves as before.

### Non-Decisions

- **No directory walk-up** for local config — only checks cwd. Simple and predictable.
- **No `config view --show-origin`** in initial implementation (deferred polish).
- **No `config check` secret warning** in initial implementation (deferred polish).

## Consequences

### Positive
- Teams can distribute system-wide defaults via `/etc/xdg/grafanactl/config.yaml` or a managed file
- Per-project overrides via `.grafanactl.yaml` without touching the user config
- `.grafanactl.yaml` can be checked into a repo for project-scoped context (with care about secrets)
- `config path` gives instant visibility into which files are active — debugging "why is my config wrong?" is trivial
- `config edit` removes the "where is my config file?" friction

### Negative
- Layering adds a discovery step on every config load — negligible I/O cost (stat 3 files), but adds conceptual complexity
- `config set` without `--file` now errors when multiple configs exist — breaking behavior for users with both user and local configs
- Local config risks accidental secret commit if `cloud.token` is set per-project (mitigated by future `config check` warning)

### Neutral
- `--config` flag continues to bypass all layering — explicit always wins
- Single-config users see no behavior change
- `LoadLayered` replaces `Load` as the main entry point; `Load` is kept for single-file cases
