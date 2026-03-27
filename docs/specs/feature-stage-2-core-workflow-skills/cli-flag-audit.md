# CLI Flag Audit

Verified flag names, short aliases, and default values from Go source in `cmd/gcx/`.

**Source verification date**: 2026-03-07
**Commit audited**: HEAD of branch `t1-cli-flag-audit`

---

## Global / Persistent Flags (all commands inherit via `config.Options.BindFlags`)

Source: `cmd/gcx/config/command.go` lines 28–33

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | | `""` | Path to the configuration file to use |
| `--context` | | `""` | Name of the context to use |

---

## `gcx query`

Source: `cmd/gcx/query/command.go` lines 38–44

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `wide`, `graph`, `json`, `yaml`) |
| `--datasource` | `-d` | `""` | Datasource UID |
| `--expr` | `-e` | `""` | Query expression (PromQL / LogQL / label selector) |
| `--from` | | `""` | Start time (RFC3339, Unix timestamp, or relative like `now-1h`) |
| `--to` | | `""` | End time (RFC3339, Unix timestamp, or relative like `now`) |
| `--step` | | `""` | Query step (e.g. `15s`, `1m`) |
| `--profile-type` | | `""` | Profile type ID (pyroscope only) |
| `--max-nodes` | | `1024` | Maximum nodes in flame graph (pyroscope only) |

**NC-007 verification**: Time-range flags are `--from` / `--to` — NOT `--start` / `--end`.

---

## `gcx resources get`

Source: `cmd/gcx/resources/get.go` lines 27–36

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"text"` | Output format (`text`, `wide`, `json`, `yaml`) |
| `--on-error` | | `"fail"` | Error mode: `ignore`, `fail`, `abort` |

**Notes**: Accepts `[RESOURCE_SELECTOR]...` positional args. No `--path` flag.

---

## `gcx resources push`

Source: `cmd/gcx/resources/push.go` lines 28–35

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--path` | `-p` | `"./resources"` | Paths on disk from which to read resources (comma-separated or repeated) |
| `--max-concurrent` | | `10` | Maximum concurrent operations |
| `--on-error` | | `"fail"` | Error mode: `ignore`, `fail`, `abort` |
| `--dry-run` | | `false` | Simulate push without creating/updating resources |
| `--omit-manager-fields` | | `false` | Do not append manager fields to resources |
| `--include-managed` | | `false` | Include resources managed by other tools |

---

## `gcx resources pull`

Source: `cmd/gcx/resources/pull.go` lines 27–38

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"json"` | Output format (`json`, `yaml`) — controls on-disk file format |
| `--on-error` | | `"fail"` | Error mode: `ignore`, `fail`, `abort` |
| `--path` | `-p` | `"./resources"` | Path on disk where resources will be written |
| `--include-managed` | | `false` | Include resources managed by other tools |

---

## `gcx resources delete`

Source: `cmd/gcx/resources/delete.go` lines 30–37

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--on-error` | | `"fail"` | Error mode: `ignore`, `fail`, `abort` |
| `--max-concurrent` | | `10` | Maximum concurrent operations |
| `--force` | | `false` | Delete all resources of the specified types (required for kind-only selectors) |
| `--dry-run` | | `false` | Simulate delete without removing resources |
| `--path` | `-p` | `nil` | Path(s) on disk containing resources to delete |
| `--yes` | `-y` | `false` | Auto-approve destructive operations (auto-enables `--force`) |

---

## `gcx resources edit`

Source: `cmd/gcx/resources/edit.go` lines 21–24

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"json"` | Output format for the editor buffer (`json`, `yaml`) |

**Notes**: Takes exactly one `RESOURCE_SELECTOR` positional arg. Uses `$EDITOR` env var.

---

## `gcx resources validate`

Source: `cmd/gcx/resources/validate.go` lines 28–37

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"text"` | Output format (`text`, `json`, `yaml`) |
| `--path` | `-p` | `"./resources"` | Paths on disk from which to read resources |
| `--max-concurrent` | | `10` | Maximum concurrent operations |
| `--on-error` | | `"fail"` | Error mode: `ignore`, `fail`, `abort` |

---

## `gcx resources serve`

Source: `cmd/gcx/resources/serve.go` lines 38–46

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--address` | | `"0.0.0.0"` | Address to bind |
| `--port` | | `8080` | Port on which the server will listen |
| `--watch` | `-w` | `nil` | Additional paths to watch for changes |
| `--no-watch` | | `false` | Disable file watching |
| `--script` | `-S` | `""` | Script to execute to generate a resource |
| `--script-format` | `-f` | `"json"` | Format of data returned by the script |
| `--max-concurrent` | | `10` | Maximum concurrent operations |

**Notes**: Positional args are resource directories to serve.

---

## `gcx resources list`

Source: `cmd/gcx/resources/list.go` lines 22–28

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"text"` | Output format (`text`, `wide`) |

---

## `gcx datasources list`

Source: `cmd/gcx/datasources/list.go` lines 23–29

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `json`, `yaml`) |
| `--type` | `-t` | `""` | Filter by datasource type (e.g. `prometheus`, `loki`) |

---

## `gcx datasources get`

Source: `cmd/gcx/datasources/get.go` lines 17–20

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"yaml"` | Output format (`yaml`, `json`) |

**Notes**: Takes exactly one positional `UID` arg.

---

## `gcx datasources prometheus labels`

Source: `cmd/gcx/datasources/prometheus.go` lines 37–44

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `json`, `yaml`) |
| `--datasource` | `-d` | `""` | Datasource UID |
| `--label` | `-l` | `""` | Get values for this label (omit to list all labels) |

---

## `gcx datasources prometheus metadata`

Source: `cmd/gcx/datasources/prometheus.go` lines 153–160

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `json`, `yaml`) |
| `--datasource` | `-d` | `""` | Datasource UID |
| `--metric` | `-m` | `""` | Filter by metric name |

---

## `gcx datasources prometheus targets`

Source: `cmd/gcx/datasources/prometheus.go` lines 264–271

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `json`, `yaml`) |
| `--datasource` | `-d` | `""` | Datasource UID |
| `--state` | | `""` | Filter by target state: `active`, `dropped`, `any` (default: active) |

---

## `gcx datasources loki labels`

Source: `cmd/gcx/datasources/loki.go` lines 35–42

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `json`, `yaml`) |
| `--datasource` | `-d` | `""` | Datasource UID |
| `--label` | `-l` | `""` | Get values for this label (omit to list all labels) |

---

## `gcx datasources loki series`

Source: `cmd/gcx/datasources/loki.go` lines 150–157

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"table"` | Output format (`table`, `json`, `yaml`) |
| `--datasource` | `-d` | `""` | Datasource UID |
| `--match` | `-M` | `nil` | LogQL stream selector (required, repeatable) |

---

## `gcx config view`

Source: `cmd/gcx/config/command.go` lines 188–199

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output` | `-o` | `"yaml"` | Output format (`yaml`, `json`) |
| `--minify` | | `false` | Remove all information not used by current-context |
| `--raw` | | `false` | Display sensitive information (secrets unredacted) |

---

## `gcx config set`

Source: `cmd/gcx/config/command.go` lines 450–481

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| *(none)* | | | Takes exactly two positional args: `PROPERTY_NAME PROPERTY_VALUE` |

---

## `gcx config use-context`

Source: `cmd/gcx/config/command.go` lines 418–448

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| *(none)* | | | Takes exactly one positional arg: `CONTEXT_NAME` |

Alias: `use`

---

## `gcx config current-context`

Source: `cmd/gcx/config/command.go` lines 256–276

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| *(none)* | | | No flags. Prints current context name to stdout. |

---

## `--on-error` Shared Flag Reference

Source: `cmd/gcx/resources/onerror.go` lines 25–36

Used by: `get`, `push`, `pull`, `delete`, `validate`

| Value | Behavior |
|-------|----------|
| `ignore` | Continue processing all resources, exit 0 even if some failed |
| `fail` | Continue processing all resources, exit 1 if any failed **(default)** |
| `abort` | Stop on first error, exit 1 |

---

## Key Findings for Subsequent Tasks

1. **`gcx query` time-range flags are `--from` / `--to`** — confirmed at `cmd/gcx/query/command.go:40-41`. The `--start` / `--end` names do NOT exist in the source.
2. **`--dry-run`** exists on `push` and `delete`, not on `pull` or `validate`.
3. **`--include-managed`** exists on `push` and `pull`.
4. **`--omit-manager-fields`** exists only on `push`.
5. **`--force`** exists only on `delete`.
6. **`--path` / `-p`** exists on `push` (multi-value `StringSlice`), `pull` (single `String`), `delete` (multi-value `StringSlice`), and `validate` (multi-value `StringSlice`). Default is `"./resources"` for `push`, `pull`, and `validate`; `nil` for `delete`.
7. **`--max-concurrent`** exists on `push`, `delete`, `validate`, and `serve`. Default: `10`.
8. **`serve` uses `--address` and `--port`** (not `--host`). Also has `--watch` (`-w`), `--no-watch`, `--script` (`-S`), `--script-format` (`-f`).
9. **`--output` / `-o`** is the universal output flag across all commands.
10. **`resources list`** lists available Grafana API resource types (not resource instances) — different from `resources get`.
