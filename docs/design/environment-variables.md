# Environment Variable Reference

> Canonical reference for all environment variables recognized by gcx. Other docs should link here rather than duplicating this list.

---

## 10. Environment Variable Reference

Context-scoped variables are applied after `--context` selection to the
selected context's in-memory view. They do not mutate another context and are
never persisted implicitly by login, provider, or datasource write-back paths.
`GRAFANA_CLOUD_*` authentication variables synthesize an ephemeral Cloud entry
for that invocation. When an endpoint override changes a credential
destination, supply the corresponding credential override in the same
invocation. Login commands turn one supplied endpoint into a coherent OAuth/API
pair; set both endpoint variables when a custom environment deliberately uses
distinct origins.

Auto-discovered repository `.gcx.yaml` files cannot attach runtime secrets or
external mTLS client keypairs to destinations the file supplies. Select the
file explicitly with `--config .gcx.yaml` or `GCX_CONFIG` to authorize it.
Provider-specific direct endpoint overrides must be paired with their matching
credential environment variable and are resolved from the same config
snapshot. That pair still does not authorize an auto-discovered repository
stack because its TLS and proxy settings affect the transport.

### Core Variables

| Variable | Scope | Effect |
|----------|-------|--------|
| `GRAFANA_SERVER` | context | Grafana server URL |
| `GRAFANA_TOKEN` | context | API token (this takes precedence over user/pass) |
| `GRAFANA_USER` | context | Basic auth username |
| `GRAFANA_PASSWORD` | context | Basic auth password |
| `GRAFANA_ORG_ID` | context | On-prem org ID (namespace) |
| `GRAFANA_STACK_ID` | context | Cloud stack ID (namespace) |
| `GRAFANA_PROXY_ENDPOINT` | context | Assistant backend used for Grafana OAuth proxy routing |
| `GRAFANA_TLS_CERT_FILE` | context | mTLS client certificate file |
| `GRAFANA_TLS_KEY_FILE` | context | mTLS client private-key file |
| `GRAFANA_TLS_CA_FILE` | context | Custom CA bundle file |
| `GRAFANA_CLOUD_TOKEN` | context | Cloud Access Policy token on an ephemeral Cloud entry |
| `GRAFANA_CLOUD_API_URL` | context | Grafana Cloud API destination on the ephemeral entry |
| `GRAFANA_CLOUD_OAUTH_URL` | context | Grafana Cloud OAuth issuer on the ephemeral entry |
| `GRAFANA_CLOUD_STACK` | context | Selected stack's Grafana Cloud slug |
| `GCX_CONFIG` | global | Config file path override |
| `GCX_TELEMETRY` | global | `enabled`, `disabled`, or `log`; takes precedence over `DO_NOT_TRACK` and config |
| `DO_NOT_TRACK` | global | Disable anonymous telemetry when `1` or `true` unless `GCX_TELEMETRY` overrides it |
| `GCX_NO_UPDATE_NOTIFIER` | global | Disable the periodic gcx/skill update notifier when non-empty |
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
| `GCX_AGENT_SPILL_BYTES` | Output tuning | Spill threshold in bytes for the `agents` codec (default `102400` = 100 KiB). Payloads above this are written to a temp file; a summary is printed instead. Invalid values fall back to the default. See [output.md § Agents Codec](output.md#111-agents-codec) |
| `CLAUDECODE` | Claude Code | Truthy value activates agent mode |
| `CLAUDE_CODE` | Claude Code | Truthy value activates agent mode |
| `CURSOR_AGENT` | Cursor | Truthy value activates agent mode |
| `GITHUB_COPILOT` | GitHub Copilot | Truthy value activates agent mode |
| `AMAZON_Q` | Amazon Q | Truthy value activates agent mode |

Detection runs at `init()` time in `internal/agent/agent.go`. See [agent-mode.md § Detection](agent-mode.md#61-detection) for
full detection priority and the `--agent` flag.
