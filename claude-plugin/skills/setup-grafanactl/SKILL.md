---
name: setup-grafanactl
description: >
  Use this skill to set up grafanactl, configure authentication, establish a
  connection to a Grafana instance, and complete first-time configuration. This
  skill covers Grafana Cloud and on-premise deployments, environment variable
  overrides for CI/CD, default datasource configuration, and troubleshooting
  connection and authentication problems.
---

# Setup grafanactl

This skill teaches agents to configure grafanactl for a Grafana instance,
covering the three configuration paths (Grafana Cloud, on-premise, and
environment variables) and resolving common setup errors.

## Step 0: Install grafanactl

First, check whether grafanactl is already installed:

```bash
grafanactl version
```

If the command is not found, build it from source. Requires
[git](https://git-scm.com/) and [Go](https://go.dev/) v1.24+:

```bash
go install github.com/grafana/grafanactl-experiments/cmd/grafanactl@latest
```

After installing, verify the binary is on PATH:

```bash
grafanactl version
```

## Configuration Model

grafanactl uses a context-based configuration model inspired by kubectl's
kubeconfig. A single YAML file (default: `~/.config/grafanactl/config.yaml`)
stores named contexts. Each context points to one Grafana instance and holds
the server URL, authentication credentials, and namespace identifiers. One
context is active at a time; all commands operate against it unless overridden.

Use `grafanactl config view` to inspect the current configuration at any time.
Use `grafanactl config check` to validate that the active context is correct
and can reach the server.

---

## Path A: Grafana Cloud

Use this path when connecting to a Grafana Cloud instance
(URLs ending in `.grafana.net`).

### Step 1: Create a context

```bash
grafanactl config set contexts.cloud.grafana.server https://myorg.grafana.net
```

Replace `cloud` with any name you prefer for this context (e.g., `prod`,
`myorg-cloud`). Replace the server URL with your Grafana Cloud URL.

### Step 2: Set the API token

```bash
grafanactl config set contexts.cloud.grafana.token glsa_XXXXXXXXXXXXXXXX
```

Obtain a service account token from **Administration > Service accounts** in
your Grafana Cloud instance. The token must have sufficient permissions for the
operations you intend to run (Viewer for read-only, Editor or Admin for write
operations).

The `grafana.token` field takes precedence over `grafana.user`/`grafana.password`
when both are present.

### Step 3: Switch to the context

```bash
grafanactl config use-context cloud
```

### Step 4: Verify the connection

```bash
grafanactl config check
```

A successful check prints the active context name and server URL without
errors. For Grafana Cloud, the stack ID (namespace) is auto-discovered from
the server's `/bootdata` endpoint -- you do not need to set `grafana.stack-id`
manually unless auto-discovery fails.

**Namespace note**: grafanactl maps Grafana Cloud instances to a Kubernetes
namespace of the form `stacks-<id>`. This namespace is discovered automatically
by calling the `/bootdata` endpoint on the server URL. If the discovered stack
ID conflicts with a manually configured `grafana.stack-id`, grafanactl raises a
validation error. To resolve: either remove the manually configured stack ID
(`grafanactl config unset contexts.cloud.grafana.stack-id`) or correct it to
match the discovered value.

---

## Path B: On-Premise Grafana

Use this path when connecting to a self-hosted Grafana instance.

### Step 1: Create a context

```bash
grafanactl config set contexts.onprem.grafana.server https://grafana.example.com
```

Replace `onprem` with a name that identifies this environment (e.g.,
`production`, `staging`, `local`).

### Step 2: Set authentication

**Option B-1: API token (recommended)**

```bash
grafanactl config set contexts.onprem.grafana.token glsa_XXXXXXXXXXXXXXXX
```

**Option B-2: Username and password**

```bash
grafanactl config set contexts.onprem.grafana.user admin
grafanactl config set contexts.onprem.grafana.password mysecretpassword
```

Use Option B-1 when service accounts are available. Use Option B-2 for
development or when service accounts are not configured.

### Step 3: Set the org ID

On-premise Grafana uses an org ID to identify the namespace for API calls.
Set it to the numeric ID of the organization (default org is 1):

```bash
grafanactl config set contexts.onprem.grafana.org-id 1
```

To find the org ID: in Grafana, go to **Administration > Organizations** and
note the numeric ID shown in the URL when you select an org.

### Step 4: Switch to the context

```bash
grafanactl config use-context onprem
```

### Step 5: Verify the connection

```bash
grafanactl config check
```

**TLS options** (optional): If your Grafana instance uses a self-signed
certificate or a custom CA, configure TLS:

```bash
# Skip TLS verification (development only -- do not use in production)
grafanactl config set contexts.onprem.grafana.tls.insecure-skip-verify true

# Supply a custom CA certificate (base64-encoded PEM)
grafanactl config set contexts.onprem.grafana.tls.ca-data <base64-encoded-pem>
```

---

## Path C: Environment Variables (CI/CD)

Use this path when grafanactl runs in a CI/CD pipeline or another automated
environment where writing a config file is impractical. Environment variables
override the active context's fields at runtime without modifying the config
file.

| Environment Variable  | Overrides Field       | Description                          |
|-----------------------|-----------------------|--------------------------------------|
| `GRAFANA_SERVER`      | `grafana.server`      | Server URL                           |
| `GRAFANA_TOKEN`       | `grafana.token`       | API token (takes precedence over user/pass) |
| `GRAFANA_USER`        | `grafana.user`        | Username for basic auth              |
| `GRAFANA_PASSWORD`    | `grafana.password`    | Password for basic auth              |
| `GRAFANA_ORG_ID`      | `grafana.org-id`      | Org ID (on-premise namespace)        |
| `GRAFANA_STACK_ID`    | `grafana.stack-id`    | Stack ID (Grafana Cloud namespace)   |

### Example: GitHub Actions

```yaml
- name: Run grafanactl
  env:
    GRAFANA_SERVER: ${{ secrets.GRAFANA_SERVER }}
    GRAFANA_TOKEN: ${{ secrets.GRAFANA_TOKEN }}
    GRAFANA_ORG_ID: "1"
  run: grafanactl resources get dashboards -o json
```

Environment variables apply to the **current context** only and do not
modify the config file on disk.

### Config file location

If you need to supply a config file path explicitly:

```bash
grafanactl --config /path/to/config.yaml resources get dashboards
# or
export GRAFANACTL_CONFIG=/path/to/config.yaml
```

Config file search order (highest to lowest priority):

1. `--config <path>` CLI flag
2. `$GRAFANACTL_CONFIG` environment variable
3. `$XDG_CONFIG_HOME/grafanactl/config.yaml`
4. `$HOME/.config/grafanactl/config.yaml`
5. `$XDG_CONFIG_DIRS/grafanactl/config.yaml`

---

## Default Datasource Configuration

To avoid passing `-d <uid>` on every query command, configure default
datasource UIDs for the active context.

### Find your datasource UIDs

```bash
grafanactl datasources list -o json
```

Locate the `uid` field for each datasource. Example output:

```json
{
  "datasources": [
    { "uid": "prometheus-uid-abc123", "name": "Prometheus", "type": "prometheus" },
    { "uid": "loki-uid-def456",       "name": "Loki",       "type": "loki"       }
  ]
}
```

### Set defaults

```bash
# Set the default Prometheus datasource
grafanactl config set contexts.cloud.default-prometheus-datasource prometheus-uid-abc123

# Set the default Loki datasource
grafanactl config set contexts.cloud.default-loki-datasource loki-uid-def456
```

Replace `cloud` with your context name and the UID values with those from the
output above. After setting these, query commands that support a `-d` flag will
use the configured defaults automatically.

---

## Multi-Context Management

To work with multiple Grafana environments, create a context for each:

```bash
# Create contexts
grafanactl config set contexts.production.grafana.server https://grafana.example.com
grafanactl config set contexts.production.grafana.token glsa_PROD_TOKEN
grafanactl config set contexts.production.grafana.org-id 1

grafanactl config set contexts.staging.grafana.server https://grafana-staging.example.com
grafanactl config set contexts.staging.grafana.token glsa_STAGING_TOKEN
grafanactl config set contexts.staging.grafana.org-id 1

# Switch between contexts
grafanactl config use-context production
grafanactl config use-context staging

# Use a context for a single command without switching
grafanactl --context staging resources get dashboards
```

To list all configured contexts, view the full config:

```bash
grafanactl config view
```

Secrets (`token`, `password`) are redacted in this output. To see raw values:

```bash
grafanactl config view --raw
```

---

## Troubleshooting

### config check fails

Run `grafanactl config check` to diagnose configuration problems. It prints the
active context and performs a live health check against the server.

If it reports a missing server or empty context:

```bash
# Verify current context is set
grafanactl config view

# Ensure the current-context field is not empty
grafanactl config set current-context <your-context-name>
```

If it reports a missing namespace (stack ID or org ID):

- **Grafana Cloud**: either let auto-discovery resolve it (no manual action
  needed for `.grafana.net` URLs) or set `grafana.stack-id` explicitly.
- **On-premise**: set `grafana.org-id` to the numeric org ID (usually `1`).

### 401 Unauthorized

The token or credentials are invalid or expired.

```bash
# Replace with a fresh token
grafanactl config set contexts.<name>.grafana.token glsa_NEW_TOKEN
```

Verify the token has not expired and has the correct permissions for the
operations you intend to run.

### 403 Forbidden

The token is valid but lacks permissions for the requested operation. In
Grafana, navigate to **Administration > Service accounts**, select the service
account, and assign an appropriate role (Viewer, Editor, or Admin).

### Connection refused or timeout

The server URL is unreachable.

1. Confirm the URL is correct:

   ```bash
   grafanactl config view
   ```

2. Test connectivity from the machine running grafanactl:

   ```bash
   curl -I https://grafana.example.com/api/health
   ```

3. Check for proxy requirements or VPN. If the instance uses a self-signed
   certificate:

   ```bash
   grafanactl config set contexts.<name>.grafana.tls.insecure-skip-verify true
   ```

   Use `insecure-skip-verify` only for development; supply a CA certificate in
   production environments instead.

### Namespace resolution issues

grafanactl resolves the API namespace (Kubernetes namespace for all calls) in
this order:

1. Attempt auto-discovery via `/bootdata` HTTP call to the server
2. If discovery fails and `org-id` is non-zero: use `org-<id>` namespace
3. If discovery fails and `org-id` is zero: use configured `stack-id`

If you see a "mismatched stack ID" error, a configured `grafana.stack-id`
differs from the auto-discovered value. Resolve by unsetting the manual value:

```bash
grafanactl config unset contexts.<name>.grafana.stack-id
```

If you see a "missing namespace" error and auto-discovery is failing (e.g.,
the server does not expose `/bootdata`), set the namespace manually:

```bash
# On-premise
grafanactl config set contexts.<name>.grafana.org-id 1

# Grafana Cloud (if auto-discovery is unavailable)
grafanactl config set contexts.<name>.grafana.stack-id 12345
```

---

## Complete Example: Grafana Cloud Setup

```bash
# 1. Set server and token
grafanactl config set contexts.mycloud.grafana.server https://myorg.grafana.net
grafanactl config set contexts.mycloud.grafana.token glsa_XXXXXXXXXXXXXXXX

# 2. Activate the context
grafanactl config use-context mycloud

# 3. Verify
grafanactl config check

# 4. Set default datasources (after listing available ones)
grafanactl datasources list -o json
grafanactl config set contexts.mycloud.default-prometheus-datasource <prometheus-uid>
grafanactl config set contexts.mycloud.default-loki-datasource <loki-uid>

# 5. Test a resource listing
grafanactl resources get dashboards -o json
```

---

## Reference

For a complete listing of all config set paths, environment variables,
namespace resolution logic, and multi-context patterns, see
`references/configuration.md`.
