# grafanactl Configuration Reference

grafanactl uses a context-based configuration model inspired by kubectl's kubeconfig. A single YAML
file holds named contexts, each describing a Grafana instance. One context is "current" at any time;
all commands operate against it unless overridden.

---

## Config File Location

grafanactl searches for the config file in the following order (highest priority first):

| Priority | Source |
|----------|--------|
| 1 | `--config <path>` CLI flag |
| 2 | `$GRAFANACTL_CONFIG` environment variable |
| 3 | `$XDG_CONFIG_HOME/grafanactl/config.yaml` |
| 4 | `$HOME/.config/grafanactl/config.yaml` |
| 5 | `$XDG_CONFIG_DIRS/grafanactl/config.yaml` (e.g., `/etc/xdg/grafanactl/config.yaml`) |

If no file is found, an empty one is created at the standard location with a single `default` context.

---

## Config File Structure

```yaml
current-context: "production"

contexts:
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
    grafana:
      server: "https://myorg.grafana.net"
      token: "glsa_yyyy"
      stack-id: 12345

  # Local dev with basic auth
  local:
    grafana:
      server: "http://localhost:3000"
      user: "admin"
      password: "admin"
      org-id: 1
```

---

## Config Set Paths

Use `grafanactl config set <path> <value>` to write individual fields. Paths use dot-separated YAML
tag names. If a context does not exist, it is created automatically.

### Grafana Connection

| Path | YAML Key | Description |
|------|----------|-------------|
| `contexts.<name>.grafana.server` | `server` | Grafana server URL (required) |
| `contexts.<name>.grafana.token` | `token` | Service account API token (takes precedence over user/password) |
| `contexts.<name>.grafana.user` | `user` | Username for basic auth |
| `contexts.<name>.grafana.password` | `password` | Password for basic auth (redacted in `config view`) |
| `contexts.<name>.grafana.org-id` | `org-id` | Organization ID for on-prem Grafana (maps to namespace `org-N`) |
| `contexts.<name>.grafana.stack-id` | `stack-id` | Stack ID for Grafana Cloud (maps to namespace `stacks-N`; can be omitted if auto-discovery succeeds) |

### TLS

| Path | YAML Key | Description |
|------|----------|-------------|
| `contexts.<name>.grafana.tls.insecure-skip-verify` | `insecure-skip-verify` | Disable TLS certificate validation (bool) |
| `contexts.<name>.grafana.tls.ca-data` | `ca-data` | Custom CA bundle (base64-encoded PEM) |
| `contexts.<name>.grafana.tls.cert-data` | `cert-data` | Client certificate (base64-encoded PEM) |
| `contexts.<name>.grafana.tls.key-data` | `key-data` | Client certificate key (base64-encoded PEM; redacted in `config view`) |

### Datasource Defaults

| Path | YAML Key | Description |
|------|----------|-------------|
| `contexts.<name>.default-prometheus-datasource` | `default-prometheus-datasource` | UID of default Prometheus datasource |
| `contexts.<name>.default-loki-datasource` | `default-loki-datasource` | UID of default Loki datasource |

### Examples

```bash
grafanactl config set contexts.production.grafana.server https://grafana.example.com
grafanactl config set contexts.production.grafana.token glsa_xxxx
grafanactl config set contexts.production.grafana.org-id 1
grafanactl config set contexts.production.grafana.tls.insecure-skip-verify true
grafanactl config set contexts.local.grafana.user admin
grafanactl config set contexts.local.grafana.password admin
grafanactl config set contexts.cloud.grafana.stack-id 12345
grafanactl config set contexts.production.default-prometheus-datasource <uid>
grafanactl config unset contexts.production.grafana.password
```

---

## Environment Variables

Environment variables patch the **current context only** at load time. They do not affect other
contexts and never mutate the config file.

| Variable | Config Path | Type |
|----------|-------------|------|
| `GRAFANA_SERVER` | `contexts.<current>.grafana.server` | string |
| `GRAFANA_USER` | `contexts.<current>.grafana.user` | string |
| `GRAFANA_PASSWORD` | `contexts.<current>.grafana.password` | string |
| `GRAFANA_TOKEN` | `contexts.<current>.grafana.token` | string |
| `GRAFANA_ORG_ID` | `contexts.<current>.grafana.org-id` | integer |
| `GRAFANA_STACK_ID` | `contexts.<current>.grafana.stack-id` | integer |

**Precedence:** env vars override config file values for the active context. Token takes precedence
over user/password when both are set.

```bash
# Override server and token for the current context without editing the config file
export GRAFANA_SERVER=https://grafana.example.com
export GRAFANA_TOKEN=glsa_xxxx
grafanactl resources get dashboards
```

---

## Namespace Resolution

Every API call to Grafana's Kubernetes-compatible API requires a namespace. grafanactl derives it
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
grafanactl config view                  # show full config (secrets redacted)
grafanactl config view --raw            # show full config including secrets
grafanactl config current-context       # print active context name
```

### Switch context

```bash
# Permanent switch — updates current-context in the config file
grafanactl config use-context production

# Temporary override — affects only the current command
grafanactl --context staging resources get dashboards
```

### Create or update a context

```bash
grafanactl config set contexts.myctx.grafana.server https://grafana.example.com
grafanactl config set contexts.myctx.grafana.token glsa_xxxx
grafanactl config set contexts.myctx.grafana.org-id 1
grafanactl config use-context myctx
```

### Remove a context

```bash
grafanactl config unset contexts.myctx
```

---

## Authentication

grafanactl supports two authentication methods. Token takes precedence when both are configured.

**Service account token (recommended):**
```bash
grafanactl config set contexts.<name>.grafana.token glsa_xxxx
```

**Basic authentication:**
```bash
grafanactl config set contexts.<name>.grafana.user admin
grafanactl config set contexts.<name>.grafana.password admin
```

---

## Secret Redaction

`grafanactl config view` redacts sensitive fields by default:

| Field | Redacted |
|-------|---------|
| `grafana.token` | yes |
| `grafana.password` | yes |
| `grafana.tls.key-data` | yes |

Pass `--raw` to display the actual values.

---

## Quick-Start: Minimum Valid Configuration

**On-prem Grafana:**
```bash
grafanactl config set contexts.prod.grafana.server https://grafana.example.com
grafanactl config set contexts.prod.grafana.token glsa_xxxx
grafanactl config set contexts.prod.grafana.org-id 1
grafanactl config use-context prod
```

**Grafana Cloud (stack-id explicit):**
```bash
grafanactl config set contexts.cloud.grafana.server https://myorg.grafana.net
grafanactl config set contexts.cloud.grafana.token glsa_yyyy
grafanactl config set contexts.cloud.grafana.stack-id 12345
grafanactl config use-context cloud
```

**Grafana Cloud (stack-id auto-discovered):**
```bash
grafanactl config set contexts.cloud.grafana.server https://myorg.grafana.net
grafanactl config set contexts.cloud.grafana.token glsa_yyyy
grafanactl config use-context cloud
# stack-id is resolved automatically from /bootdata at runtime
```
