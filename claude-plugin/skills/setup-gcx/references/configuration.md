# gcx Configuration Reference

gcx uses a configuration model inspired by kubectl's kubeconfig. Version 1 YAML files
hold named `stacks` (Grafana connection + provider config), named `cloud`
entries (grafana.com credentials, shared across contexts), and `contexts` that bind a stack and
optionally a cloud entry together. One context is "current" at any time; all commands operate
against it unless overridden.

Version `1` is the only declared version accepted by this release. A future or
otherwise unsupported version is rejected before migration, backup creation, or
keychain access.

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

`--config <path>` or `$GCX_CONFIG` selects one explicit file and bypasses
layering. Otherwise gcx loads every existing source from lowest to highest
priority:

| Priority | Source |
|----------|--------|
| 3 (lowest) | system config (`$XDG_CONFIG_DIRS/gcx/config.yaml` or platform equivalent) |
| 2 | user config (`$HOME/.config/gcx/config.yaml`, then platform fallback) |
| 1 (highest) | repository config (`.gcx.yaml` in the current directory) |

If no file is found, an empty one is created at the standard location with a single `default` context.

Same-named `stacks` and `cloud` entries are atomic: the higher layer replaces
the whole entry. Context references and datasource defaults may merge. This is
a trust boundary that prevents one source from combining its endpoint with a
credential from another source.

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
    api-url: https://grafana.com  # optional production environment anchor

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

Paths are literal: they name the exact location in the file, starting from a top-level section
(`stacks.<name>.`, `cloud.<entry>.`, `contexts.<name>.`, `resources.`, `current-context`).
Nothing resolves against the current context. Bare and legacy paths (`grafana.server`,
`cloud.token`, `default-prometheus-datasource`, ...) error with the absolute path spelled out.

Editing a named stack or Cloud entry affects every context that references it.
Login commands use copy-on-write before changing a shared Cloud credential, but
a literal `gcx config set cloud.<entry>...` intentionally edits that named
entry. Destination edits clear credentials that would otherwise remain bound to
the old server or endpoint; normalization-equivalent no-ops preserve them.

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
| `cloud.<entry>.oauth-token` | `oauth-token` | Experimental direct Cloud OAuth token (redacted) |
| `cloud.<entry>.oauth-token-expires-at` | `oauth-token-expires-at` | Issuer-reported OAuth expiry (RFC3339) |
| `cloud.<entry>.oauth-scopes` | `oauth-scopes` | Granted OAuth scope set retained across keep/re-auth flows |
| `cloud.<entry>.api-url` | `api-url` | GCOM API destination |
| `cloud.<entry>.oauth-url` | `oauth-url` | OAuth issuer paired with the API destination |

A credential-bearing Cloud entry is destination-self-contained. One explicit
endpoint fills its missing peer. With neither set, gcx derives one unique Cloud
environment from referencing stack servers, using `https://grafana.com` when no
reference identifies another environment. Incompatible environments are
rejected and require separate entries.

### Examples

```bash
gcx config set stacks.production.grafana.server https://grafana.example.com
gcx config set stacks.production.grafana.token glsa_xxxx
gcx config set stacks.production.grafana.org-id 1
gcx config set stacks.production.grafana.tls.insecure-skip-verify true
gcx config set contexts.production.stack production
gcx config set contexts.production.datasources.prometheus <uid>
gcx config set cloud.grafana-com.token glc_xxxx
gcx config unset stacks.production.grafana.password
```

---

## Environment Variables

Environment variables patch the **selected context only** at load time. They do not affect other
contexts and never mutate the config file. Context selection happens before these overrides.

| Variable | Overrides | Type |
|----------|-----------|------|
| `GRAFANA_SERVER` | selected stack's `grafana.server` | string |
| `GRAFANA_USER` | selected stack's `grafana.user` | string |
| `GRAFANA_PASSWORD` | selected stack's `grafana.password` | string |
| `GRAFANA_TOKEN` | selected stack's `grafana.token` | string |
| `GRAFANA_ORG_ID` | selected stack's `grafana.org-id` | integer |
| `GRAFANA_STACK_ID` | selected stack's `grafana.stack-id` | integer |
| `GRAFANA_PROXY_ENDPOINT` | selected stack's `grafana.proxy-endpoint` | string |
| `GRAFANA_TLS_CERT_FILE` | selected stack's `grafana.tls.cert-file` | string |
| `GRAFANA_TLS_KEY_FILE` | selected stack's `grafana.tls.key-file` | string |
| `GRAFANA_TLS_CA_FILE` | selected stack's `grafana.tls.ca-file` | string |
| `GRAFANA_CLOUD_TOKEN` | cloud entry token (ephemeral entry, never persisted) | string |
| `GRAFANA_CLOUD_API_URL` | cloud entry api-url (ephemeral) | string |
| `GRAFANA_CLOUD_OAUTH_URL` | cloud entry oauth-url (ephemeral) | string |
| `GRAFANA_CLOUD_STACK` | selected stack's slug | string |

**Precedence:** env vars override config file values for the selected context. Token takes precedence
over user/password when both are set.

Credentials stored in the OS keychain are bound to their canonical source file,
exact owner kind/name, exact secret field, and normalized destination. If an
environment variable changes a server, Cloud endpoint, or Synthetic Monitoring
endpoint, the stored credential is not reused for that new destination. Supply
the corresponding credential override in the same invocation. Login commands
turn one supplied endpoint into a coherent OAuth/API pair; set both endpoint
variables when a custom environment deliberately uses distinct origins.

An automatically discovered repository `.gcx.yaml` cannot attach runtime or
new login credentials, or external mTLS client key files, to destinations and
TLS/proxy settings supplied by that file. Select it explicitly with
`--config .gcx.yaml` or `GCX_CONFIG=.gcx.yaml` after review. Direct provider
endpoint overrides require the corresponding runtime credential too, but that
pair does not authorize repository-controlled TLS or proxy configuration.

```bash
# Override server and token for the selected context without editing the config file
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
1. A configured `stack-id` is authoritative → namespace `stacks-N` without a
   discovery request.
2. Otherwise, try the memoized `/bootdata` discovery call.
   → success: use discovered stack-id → namespace `stacks-N` (even when an
   `org-id` is configured)
3. If discovery fails and `org-id != 0` → namespace `org-N`.
4. With neither usable ID → unresolved cloud namespace.
```

| Deployment | Config Field | Namespace Format |
|------------|-------------|-----------------|
| On-prem Grafana | `org-id: 1` | `org-1` |
| Grafana Cloud | `stack-id: 12345` | `stacks-12345` |
| Grafana Cloud (auto) | neither (auto-discovery) | `stacks-<discovered>` |

**Validation rules:**
- `stack-id` set → valid without a new discovery request; if a successful
  discovery is already cached and differs, validation reports the mismatch
- `org-id` set → validation skips discovery, while runtime namespace resolution
  can still prefer a successfully discovered Cloud stack ID
- Discovery succeeds with neither ID configured → valid (use discovered ID)
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

Grafana instance authentication supports browser OAuth (`gcx login`), service
account tokens, basic authentication, and mTLS client certificates. A service
account token is recommended for automation:

**Service account token:**
```bash
gcx config set stacks.<name>.grafana.token glsa_xxxx
```

**Basic authentication:**
```bash
gcx config set stacks.<name>.grafana.user admin
gcx config set stacks.<name>.grafana.password admin
```

For browser OAuth, run `gcx login <context> --oauth`; gcx stores the access and
refresh credentials plus proxy endpoint on the context's stack entry. For mTLS,
configure `grafana.tls.cert-file`, `key-file`, and optionally `ca-file`.

Grafana Cloud platform authentication is separate. A CAP lives in
`cloud.<entry>.token`; experimental direct Cloud OAuth lives in
`cloud.<entry>.oauth-token` with expiry, scopes, and endpoint metadata. Use a
CAP when full Cloud-product command compatibility is required.

---

## Secret Redaction

`gcx config view` redacts sensitive fields by default:

| Field | Redacted |
|-------|---------|
| `grafana.token` | yes |
| `grafana.password` | yes |
| `grafana.oauth-token` | yes |
| `grafana.oauth-refresh-token` | yes |
| `grafana.tls.key-data` | yes |
| `cloud.<entry>.token` | yes |
| `cloud.<entry>.oauth-token` | yes |
| declared provider secrets such as `stacks.<name>.providers.synth.sm-token` | yes |

Pass `--raw` to display the actual values.

---

## Legacy Config Migration

Pre-versioned configs (every context carrying `grafana`/`cloud`/`providers` inline) are
converted into same-named stack entries, named `cloud:` entries, and datasource defaults.
Single-source migration keeps a write-once, mode-0600 `<file>.legacy.bak` backup. Layered
migration first proves that the old field-level result is representable under atomic entries;
safe multi-source layers convert in memory only and must then be migrated one explicit layer at a
time. Only semantic conflicts or unsafe overlap between legacy and versioned entries fail before
any file or credential changes. Follow the reported migration guidance instead of moving legacy
keychain sentinel strings by hand.

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
