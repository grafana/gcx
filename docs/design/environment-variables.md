# Environment Variable Reference

> Canonical reference for all environment variables recognized by gcx. Other docs should link here rather than duplicating this list.

---

## 10. Environment Variable Reference

### Core Variables

| Variable | Scope | Effect |
|----------|-------|--------|
| `GRAFANA_SERVER` | context | Grafana server URL |
| `GRAFANA_TOKEN` | context | API token (this takes precedence over user/pass) |
| `GRAFANA_USER` | context | Basic auth username |
| `GRAFANA_PASSWORD` | context | Basic auth password |
| `GRAFANA_ORG_ID` | context | On-prem org ID (namespace) |
| `GRAFANA_STACK_ID` | context | Cloud stack ID (namespace) |
| `GCX_CONFIG` | global | Config file path override |
| `NO_COLOR` | global | Disable color output ([no-color.org](https://no-color.org/)) |

### Provider Variables

Pattern: `GRAFANA_PROVIDER_{NAME}_{KEY}=value`

| Variable | Provider | Key |
|----------|----------|-----|
| `GRAFANA_PROVIDER_SLO_TOKEN` | slo | token |
| `GRAFANA_PROVIDER_SLO_ORG_ID` | slo | org-id |
| `GRAFANA_PROVIDER_SM_TOKEN` | sm | token |
| `GRAFANA_PROVIDER_SM_URL` | sm | url |

Provider names and keys are case-normalized. Env vars override YAML config.

See [../architecture/config-system.md](../architecture/config-system.md) for the loading chain and
[../reference/provider-guide.md](../reference/provider-guide.md) for the `ConfigKeys()` pattern.

### Implemented Variables

| Variable | Effect | Documentation |
|----------|--------|---------------|
| `GCX_AUTO_APPROVE` | Auto-enable `--force` on delete operations | See `docs/reference/environment-variables/` |

Accepts: `1`, `true`, `0`, `false` (parsed by `caarlos0/env/v11`)

**Implementation:** `internal/config/cli_options.go` - `CLIOptions` struct loaded via `LoadCLIOptions()`

### Agent Mode Variables

| Variable | Source | Effect |
|----------|--------|--------|
| `GCX_AGENT_MODE` | Explicit opt-in/out | `1`/`true`/`yes` enables agent mode; `0`/`false`/`no` disables (overrides all others) |
| `CLAUDECODE` | Claude Code | Truthy value activates agent mode |
| `CLAUDE_CODE` | Claude Code | Truthy value activates agent mode |
| `CURSOR_AGENT` | Cursor | Truthy value activates agent mode |
| `GITHUB_COPILOT` | GitHub Copilot | Truthy value activates agent mode |
| `AMAZON_Q` | Amazon Q | Truthy value activates agent mode |

Detection runs at `init()` time in `internal/agent/agent.go`. See [agent-mode.md § Detection](agent-mode.md#61-detection) for
full detection priority and the `--agent` flag.
