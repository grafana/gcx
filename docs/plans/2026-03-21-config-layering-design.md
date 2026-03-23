# Config Layering and Management Commands — Design

## Problem Statement

grafanactl loads config from a single file (first-found XDG path). There is no
mechanism to layer configs from multiple sources (system defaults, user prefs,
project-local overrides). Users cannot see which config file is loaded or open
it in an editor.

## Design

### Config Layering

The loader discovers and deep-merges configs from up to three file sources,
plus env var overrides:

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

**Layer types**: `system`, `user`, `local`

**Deep merge rules**:
- Scalar fields: higher priority wins
- Maps (contexts, providers, datasources): merge by key
- Struct fields within a context: higher priority wins per-field
- `current-context`: highest layer that sets it wins
- `nil`/omitempty fields in higher layers do not erase lower layer values

**Source tracking**: The merged Config gains a `Sources []ConfigSource`:

```go
type ConfigSource struct {
    Path    string    // absolute file path
    Type    string    // "system", "user", "local", "explicit"
    ModTime time.Time // file modification time
}
```

Files that don't exist are skipped (no error). If zero files exist, auto-create
the user-level config with a default context (preserving current behavior).

### Load Pipeline

```
Today:
  --config flag OR first-found XDG path  →  Load()  →  Config  →  Overrides(env)

Proposed:
  DiscoverSources()    →  LoadLayered()  →  Config{.Sources}  →  Overrides(env)
  ├─ system XDG             ├─ Load each source independently
  ├─ user XDG               ├─ Deep-merge in order
  └─ local .grafanactl.yaml └─ Attach source metadata
```

### `config path` Command

Shows loaded config files with type, priority, and modification time.

```
$ grafanactl config path
PATH                                                 TYPE    PRIORITY  MODIFIED
/Users/igor/Code/myproject/.grafanactl.yaml          local   1 (highest)  2026-03-21 09:10:05
/Users/igor/.config/grafanactl/config.yaml           user    2            2026-03-21 08:45:12
/Library/Application Support/grafanactl/config.yaml  system  3 (lowest)   2026-03-15 10:22:31
```

- Shows only files that exist on disk
- Highest priority first
- Respects `--output` flag (table/json/yaml) and agent mode
- If `--config` flag is used, shows only that file with type `explicit`

### `config edit` Command

Opens a config file in `$EDITOR`.

```
$ grafanactl config edit              # error if multiple configs loaded
$ grafanactl config edit user         # opens user config
$ grafanactl config edit local --create  # creates .grafanactl.yaml if missing, opens it
```

- Positional arg selects layer type (`system`, `user`, `local`)
- If only one config exists: opens directly (no arg needed)
- If multiple exist and no arg: errors with list of available types
- `--create`: creates file with empty default config if missing
- Uses `$EDITOR`, falls back to `vi` (Linux/macOS) or `notepad` (Windows)
- Edits file in place (not temp-file round-trip)
- No validation on save — user can fix with `config check`

### Modified Commands

**`config set` / `config unset`**:
- Add `--file <type>` flag to target a specific layer
- Error if multiple configs detected and no `--file` flag specified
- Example: `grafanactl config set --file local contexts.dev.cloud.token xxx`

**`config view`**:
- Add `--show-origin` flag to annotate merged output with source layer
  (similar to `git config --show-origin`)

### Edge Cases

- **No config files**: `config path` shows empty table. `config edit user --create`
  bootstraps. Other commands auto-create user config (current behavior).
- **Dir walk-up**: No. Only checks cwd for `.grafanactl.yaml`. Simple and predictable.
- **Secrets in local config**: `config check` warns if local config contains fields
  tagged `datapolicy:"secret"` (risk of committing secrets to git).
- **`--config` flag**: Bypasses layering entirely — single file, type `explicit`.
