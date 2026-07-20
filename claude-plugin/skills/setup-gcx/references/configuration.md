# gcx Configuration Reference

gcx uses a configuration model inspired by kubectl's kubeconfig. A single YAML file
(format `version: 1`) holds named `stacks` (Grafana connection + provider config), named `cloud`
entries (grafana.com credentials, shared across contexts), and `contexts` that bind a stack and
optionally a cloud entry together. One context is "current" at any time; all commands operate
against it unless overridden.

## Contents

- [Config File Location](#config-file-location)
- [Config File Structure](#config-file-structure)
- [Config Set Paths](#config-set-paths)
- [Environment Variables](#environment-variables)
- [Namespace Resolution](#namespace-resolution)
- [Multi-Context Management](#multi-context-management)
- [Authentication](#authentication)
- [Secret Redaction](#secret-redaction)
- [Legacy Config Migration](#legacy-config-migration)
- [Quick-Start: Minimum Valid Configuration](#quick-start-minimum-valid-configuration)

---

## Config File Location

gcx searches for the config file in the following order (highest priority first):

| Priority | Source |
|----------|--------|
| 1 | `--config <path>` CLI flag |
| 2 | `$GCX_CONFIG` environment variable |
| 3 | `$XDG_CONFIG_HOME/gcx/config.yaml` |
| 4 | `$HOME/.config/gcx/config.yaml` |
| 5 | `$XDG_CONFIG_DIRS/gcx/config.yaml` (e.g., `/etc/xdg/gcx/config.yaml`) |

If no file is found, an empty one is created at the standard location with a single `default` context.

---

## Config File Structure

```yaml
version: 1
current-context: "production"

stacks:
  # On-prem Grafana with API token
  production:
    grafana:
      server: "https://grafana.example.com"
      token: "glsa_xxxx"
      org-id: 1
      tls:
        insecure-skip-verify: false
        ca-data: <base64-encoded PEM>
        cert-data: <base64-encoded PEM>
        key-data: <base64-encoded PEM>

  # Grafana Cloud with stack-id
  cloud-staging:
    slug: mystack                 # grafana.com stack slug; derived from server if unset
    grafana:
      server: "https://mystack.grafana.net"
      token: "glsa_yyyy"
      stack-id: 12345
    providers:                    # per-provider config lives on the stack
      synth:
        sm-token: "..."

  # Local dev with basic auth
  local:
    grafana:
      server: "http://localhost:3000"
      user: "admin"
      password: "admin"
      org-id: 1

cloud:
  # Named grafana.com (GCOM) auth entries, shared across contexts
  grafana-com:
    token: "glc_xxxx"             # Cloud Access Policy token
    api-url: https://grafana.com  # optional, default https://grafana.com

contexts:
  production:
    stack: production             # required for Grafana access
  cloud-staging:
    stack: cloud-staging
    cloud: grafana-com            # optional; cloud commands need it at runtime
    datasources:                  # per-kind default datasource UIDs
      prometheus: my-prom-uid
  local:
    stack: local
```

---

## Config Set Paths

Use `gcx config set <path> <value>` to write individual fields. Paths use dot-separated YAML
tag names. Missing stack, cloud, or context entries are created automatically.

Bare paths resolve through the current context: `grafana.*`, `providers.*`, and `slug` target
the current context's **stack entry** (which other contexts may share); `datasources.*`, `stack`,
and bare `cloud` target the context itself. `cloud.<entry>.<field>` is absolute. Legacy paths
(`cloud.token`, `default-prometheus-datasource`, ...) error with the new path.

### Grafana Connection

| Path | YAML Key | Description |
|------|----------|-------------|
| `stacks.<name>.grafana.server` | `server` | Grafana server URL (required) |
| `stacks.<name>.grafana.token` | `token` | Service account API token (takes precedence over user/password) |
| `stacks.<name>.grafana.user` | `user` | Username for basic auth |
| `stacks.<name>.grafana.password` | `password` | Password for basic auth (redacted in `config view`) |
| `stacks.<name>.grafana.org-id` | `org-id` | Organization ID for on-prem Grafana (maps to namespace `org-N`) |
| `stacks.<name>.grafana.stack-id` | `stack-id` | Stack ID for Grafana Cloud (maps to namespace `stacks-N`; can be omitted if auto-discovery succeeds) |
| `stacks.<name>.slug` | `slug` | grafana.com stack slug (derived from server URL if unset) |

### TLS

| Path | YAML Key | Description |
|------|----------|-------------|
| `stacks.<name>.grafana.tls.insecure-skip-verify` | `insecure-skip-verify` | Disable TLS certificate validation (bool) |
| `stacks.<name>.grafana.tls.ca-data` | `ca-data` | Custom CA bundle (base64-encoded PEM) |
| `stacks.<name>.grafana.tls.cert-data` | `cert-data` | Client certificate (base64-encoded PEM) |
| `stacks.<name>.grafana.tls.key-data` | `key-data` | Client certificate key (base64-encoded PEM; redacted in `config view`) |

### Context Bindings and Datasource Defaults

| Path | YAML Key | Description |
|------|----------|-------------|
| `contexts.<name>.stack` | `stack` | Name of the stack entry this context targets |
| `contexts.<name>.cloud` | `cloud` | Name of the cloud entry providing grafana.com auth (optional) |
| `contexts.<name>.datasources.<kind>` | `datasources` | Default datasource UID per kind (e.g. `prometheus`, `loki`) |

### Cloud Entries

| Path | YAML Key | Description |
|------|----------|-------------|
| `cloud.<entry>.token` | `token` | Cloud Access Policy token for GCOM (redacted in `config view`) |
| `cloud.<entry>.api-url` | `api-url` | GCOM base URL (optional, default `https://grafana.com`) |

### Examples

```bash
gcx config set stacks.production.grafana.server https://grafana.example.com
gcx config set stacks.production.grafana.token glsa_xxxx
gcx config set stacks.production.grafana.org-id 1
gcx config set stacks.production.grafana.tls.insecure-skip-verify true
gcx config set contexts.production.stack production
gcx config set contexts.production.datasources.prometheus <uid>
gcx config set cloud.grafana-com.token glc_xxxx
gcx config set grafana.token glsa_xxxx       # bare path: current context's stack
gcx config unset stacks.production.grafana.password
```

---

## Environment Variables

Environment variables patch the **current context only** at load time. They do not affect other
contexts and never mutate the config file.

| Variable | Overrides | Type |
|----------|-----------|------|
| `GRAFANA_SERVER` | current stack's `grafana.server` | string |
| `GRAFANA_USER` | current stack's `grafana.user` | string |
| `GRAFANA_PASSWORD` | current stack's `grafana.password` | string |
| `GRAFANA_TOKEN` | current stack's `grafana.token` | string |
| `GRAFANA_ORG_ID` | current stack's `grafana.org-id` | integer |
| `GRAFANA_STACK_ID` | current stack's `grafana.stack-id` | integer |
| `GRAFANA_CLOUD_TOKEN` | cloud entry token (ephemeral entry, never persisted) | string |
| `GRAFANA_CLOUD_API_URL` | cloud entry api-url (ephemeral) | string |
| `GRAFANA_CLOUD_STACK` | current stack's slug | string |

**Precedence:** env vars override config file values for the active context. Token takes precedence
over user/password when both are set.

```bash
# Override server and token for the current context without editing the config file
export GRAFANA_SERVER=https://grafana.example.com
export GRAFANA_TOKEN=glsa_xxxx
gcx resources get dashboards
```

---

## Namespace Resolution

Every API call to Grafana's Kubernetes-compatible API requires a namespace. gcx derives it
automatically:

```
Resolution order:
1. DiscoverStackID via /bootdata HTTP call
   → if success: use discovered stack-id → namespace "stacks-N"
   → discovery result overrides even an explicit org-id
2. If discovery fails:
   a. org-id != 0  → namespace "org-N"
   b. org-id == 0  → use configured stack-id → namespace "stacks-N"
```

| Deployment | Config Field | Namespace Format |
|------------|-------------|-----------------|
| On-prem Grafana | `org-id: 1` | `org-1` |
| Grafana Cloud | `stack-id: 12345` | `stacks-12345` |
| Grafana Cloud (auto) | neither (auto-discovery) | `stacks-<discovered>` |

**Validation rules:**
- `org-id` set → skip discovery entirely; namespace derived from org-id
- Discovery succeeds, no `stack-id` in config → valid (use discovered ID)
- Discovery succeeds, `stack-id` in config matches → valid
- Discovery succeeds, `stack-id` in config mismatches → validation error
- Discovery fails, `stack-id` in config set → valid (use configured ID)
- Discovery fails, no `stack-id`, no `org-id` → validation error

---

## Multi-Context Management

### List and inspect

```bash
gcx config view                  # show full config (secrets redacted)
gcx config view --raw            # show full config including secrets
gcx config current-context       # print active context name
```

### Switch context

```bash
# Permanent switch — updates current-context in the config file
gcx config use-context production

# Temporary override — affects only the current command
gcx --context staging resources get dashboards
```

### Create or update a context

```bash
gcx config set stacks.myctx.grafana.server https://grafana.example.com
gcx config set stacks.myctx.grafana.token glsa_xxxx
gcx config set stacks.myctx.grafana.org-id 1
gcx config set contexts.myctx.stack myctx
gcx config use-context myctx
```

### Remove a context

```bash
gcx config unset contexts.myctx
gcx config unset stacks.myctx    # also remove its stack entry if nothing else uses it
```

---

## Authentication

gcx supports two authentication methods. Token takes precedence when both are configured.

**Service account token (recommended):**
```bash
gcx config set stacks.<name>.grafana.token glsa_xxxx
```

**Basic authentication:**
```bash
gcx config set stacks.<name>.grafana.user admin
gcx config set stacks.<name>.grafana.password admin
```

---

## Secret Redaction

`gcx config view` redacts sensitive fields by default:

| Field | Redacted |
|-------|---------|
| `grafana.token` | yes |
| `grafana.password` | yes |
| `grafana.tls.key-data` | yes |
| `cloud.<entry>.token` | yes |

Pass `--raw` to display the actual values.

---

## Legacy Config Migration

Pre-versioned configs (every context carrying `grafana`/`cloud`/`providers` inline) are
migrated automatically on load: each context becomes a same-named stack entry, cloud credentials
are deduplicated into named `cloud:` entries, and `default-*-datasource` fields fold into the
`datasources:` map. A write-once `<file>.legacy.bak` backup is kept; restoring it fully rolls back.

---

## Quick-Start: Minimum Valid Configuration

**On-prem Grafana:**
```bash
gcx config set stacks.prod.grafana.server https://grafana.example.com
gcx config set stacks.prod.grafana.token glsa_xxxx
gcx config set stacks.prod.grafana.org-id 1
gcx config set contexts.prod.stack prod
gcx config use-context prod
```

**Grafana Cloud (stack-id explicit):**
```bash
gcx config set stacks.cloud.grafana.server https://myorg.grafana.net
gcx config set stacks.cloud.grafana.token glsa_yyyy
gcx config set stacks.cloud.grafana.stack-id 12345
gcx config set contexts.cloud.stack cloud
gcx config use-context cloud
```

**Grafana Cloud (stack-id auto-discovered):**
```bash
gcx config set stacks.cloud.grafana.server https://myorg.grafana.net
gcx config set stacks.cloud.grafana.token glsa_yyyy
gcx config set contexts.cloud.stack cloud
gcx config use-context cloud
# stack-id is resolved automatically from /bootdata at runtime
```
