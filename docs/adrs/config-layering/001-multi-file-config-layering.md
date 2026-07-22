# ADR-001: Multi-File Config Layering (System/User/Local)

**Created**: 2026-03-21
**Status**: accepted
**Bead**: none
**Supersedes**: none

> **Proposed amendment:** [ADR-022](../config-v1/001-versioned-split-config-and-secret-trust.md)
> would retain system/user/local discovery and explicit write targeting, but
> replace field-level merge for credential-bearing entries with atomic `stacks`
> and `cloud` entries and add a semantic preflight for layered legacy migration.
> The implementation follows that model while ratification remains pending.

## Context

gcx loads config from a single file — the first file found on the XDG search path. There is no way to:

- Apply system-wide defaults (e.g., a shared Grafana server URL for a team)
- Override specific fields per project without modifying the user config
- See which config file is actually loaded
- Open a config file in an editor without knowing its path

This becomes a pain point as users work across multiple Grafana stacks and projects. The `--config` flag is a workaround but requires remembering the path and can't be automated.

## Decision (with the proposed ADR-022 model)

### Config Layering

The loader discovers configs from up to three file sources, combines them under
the current atomic-entry rules, then applies environment/flag overrides to the
selected runtime context:

```
┌─────────────────┐  priority 3 (lowest)
│ System           │  $XDG_CONFIG_DIRS/gcx/config.yaml
└────────┬────────┘  (Linux: /etc/xdg/, macOS: /Library/Application Support/)
         ▼ merge
┌─────────────────┐  priority 2
│ User             │  $XDG_CONFIG_HOME/gcx/config.yaml
└────────┬────────┘  (default: ~/.config/)
         ▼ merge
┌─────────────────┐  priority 1 (highest)
│ Local            │  .gcx.yaml (in working directory)
└────────┬────────┘
         ▼ override functions
┌─────────────────┐
│ Env vars/flags   │  GRAFANA_*, --config, --context
└─────────────────┘

--config flag: bypasses all layering, loads only that file.
```

**Current merge rules:**

- Scalar fields such as `current-context` use the highest-priority nonempty
  value.
- Top-level maps merge by key, but each same-named `stacks` and `cloud` entry is
  atomic: the highest-priority defining layer replaces the complete entry.
  Credentials are therefore never field-merged with a destination from another
  file.
- Thin `contexts` and non-credential maps such as datasource defaults continue
  to merge by key where their schema permits it.
- `--config` bypasses discovery and makes that one file explicit/trusted.

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
$ gcx config path
PATH                                                 TYPE    PRIORITY     MODIFIED
/Users/igor/Code/myproject/.gcx.yaml          local   1 (highest)  2026-03-21 09:10:05
/Users/igor/.config/gcx/config.yaml           user    2            2026-03-21 08:45:12
/Library/Application Support/gcx/config.yaml  system  3 (lowest)   2026-03-15 10:22:31
```

Respects `--output` flag (table/json/yaml). Shows only files that exist.

### `config edit` Command

Opens a config file in `$EDITOR` (falls back to `vi` / `notepad`):

```
$ gcx config edit              # opens if only one config exists; errors if multiple
$ gcx config edit user         # opens user config by type
$ gcx config edit local --create  # creates .gcx.yaml if missing, then opens it
```

### `--file` Flag on `config set` / `config unset`

When multiple configs are loaded, `config set` requires `--file <type>` to target a specific layer:

```bash
gcx config set --file local cloud.grafana-com.token my-token
gcx config set --file user  stacks.prod.grafana.server https://prod.grafana.net
```

Without `--file` and multiple configs present: error with suggestion to add `--file`. With a single config: behaves as before.

### Non-Decisions

- **No directory walk-up** for local config — only checks cwd. Simple and predictable.
- **No `config view --show-origin`** in initial implementation (deferred polish).
- **No `config check` secret warning** in initial implementation (deferred polish).

## Consequences

### Positive
- Teams can distribute system-wide defaults via `/etc/xdg/gcx/config.yaml` or a managed file
- Per-project overrides via `.gcx.yaml` without touching the user config
- `.gcx.yaml` can be checked into a repo for project-scoped context (with care about secrets)
- `config path` gives instant visibility into which files are active — debugging "why is my config wrong?" is trivial
- `config edit` removes the "where is my config file?" friction

### Negative
- Layering adds a discovery step on every config load — negligible I/O cost (stat 3 files), but adds conceptual complexity
- `config set` without `--file` now errors when multiple configs exist — breaking behavior for users with both user and local configs
- Local config risks accidental secret commit if a `cloud.<name>.token` is set
  per-project. Auto-discovered local files cannot supply fresh runtime
  credentials or direct-provider destinations; selecting the file explicitly
  is the user's trust decision.

### Neutral
- `--config` flag continues to bypass all layering — explicit always wins
- Single-config users see no behavior change
- `LoadLayered` replaces `Load` as the main entry point; `Load` is kept for single-file cases
