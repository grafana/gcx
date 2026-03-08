# Grafana CLI (grafanactl)

**Infrastructure-as-code for Grafana, designed for automation and AI agents.**

Manage dashboards, folders, and other Grafana resources across multiple environments using a kubectl-like CLI. Works with Claude Code, GitHub Copilot, Cursor, and CI/CD pipelines.

Key features:

- **Agent-friendly:** JSON/YAML output, predictable exit codes, structured errors
- **GitOps ready:** Pull resources to files, edit locally, push back — full version control
- **Multi-environment:** Named contexts to switch between dev/staging/prod seamlessly
- **Kubernetes-style:** Familiar patterns from kubectl — contexts, selectors, resource kinds
- **Live development:** Built-in server with hot reload for dashboard-as-code workflows

> [!NOTE]
> **Grafana CLI only supports Grafana 12 and above. Older Grafana versions are not supported.**

## Quick Install

**Homebrew (macOS/Linux):**

```bash
brew install grafana/grafana/grafanactl
```

**From Release (Linux/macOS/Windows):**

```bash
# Download latest from https://github.com/grafana/grafanactl/releases
curl -L https://github.com/grafana/grafanactl/releases/latest/download/grafanactl-$(uname -s)-$(uname -m) -o grafanactl
chmod +x grafanactl
sudo mv grafanactl /usr/local/bin/
```

**Verify installation:**

```bash
grafanactl --version
```

Full installation guide: [grafanactl docs](https://grafana.github.io/grafanactl/)

## Quick Start

### Authentication

**Option A — Named context (persistent):**

```bash
grafanactl config set contexts.my-grafana.grafana.server https://your-instance.grafana.net
grafanactl config set contexts.my-grafana.grafana.token your-service-account-token
grafanactl config use-context my-grafana
```

**Option B — Environment variables (recommended for agents and CI/CD):**

```bash
export GRAFANA_SERVER="https://your-instance.grafana.net"
export GRAFANA_TOKEN="your-service-account-token"
```

**Token requirements:** Use a [Grafana service account token](https://grafana.com/docs/grafana/latest/administration/service-accounts/) with **Editor** or **Admin** role. Editor is sufficient for managing dashboards and folders; Admin is needed for data sources and other administrative resources.

**Verify connection:**

```bash
grafanactl config check
```

### Common Workflows

```bash
# Discover available resource types
grafanactl resources list

# Get all dashboards
grafanactl resources get dashboards -o json

# Get a specific dashboard
grafanactl resources get dashboards/my-dashboard -o json

# Pull dashboards to local files
grafanactl resources pull dashboards -p ./resources -o yaml

# Push local resources to Grafana
grafanactl resources push -p ./resources

# Dry-run push (simulate without changes)
grafanactl resources push -p ./resources --dry-run

# Validate resources against remote Grafana
grafanactl resources validate -p ./resources

# Delete a specific dashboard
grafanactl resources delete dashboards/my-dashboard

# Edit a dashboard interactively (opens $EDITOR)
grafanactl resources edit dashboards/my-dashboard
```

### Tips for AI Agents and Automation

- Use `-o json` on `get`, `list`, `pull`, and `validate` for machine-parseable output
- Use `--dry-run` on `push` and `delete` to preview changes before applying
- Control error behavior with `--on-error`: `abort` (fail fast), `fail` (continue, exit 1), `ignore` (continue, exit 0)
- Use `--include-managed` to modify resources created by other tools (e.g., Grafana UI)
- Use `--force` on `delete` when deleting all resources of a kind (e.g., `delete dashboards --force`)
- All commands except `edit` are non-interactive — safe to run in pipelines without hanging on prompts
- Prefer targeted selectors (`dashboards/my-dash`) over broad fetches (`dashboards`) to reduce response size and token usage — pagination is handled automatically, so all results are returned in a single response
- Use `resources list` first to discover available resource types — don't guess `apiVersion` or `kind` values
- **`get` vs `pull`:** `get` writes to stdout (for reading/inspecting), `pull` writes to files on disk (for editing and pushing back). Use `get` to inspect, `pull` to start a modify workflow.

### Common Patterns

**Check if a resource exists:**

```bash
# Exit code 0 = exists, non-zero = not found
grafanactl resources get dashboards/my-dashboard -o json > /dev/null 2>&1
```

**Pull → edit → push round-trip:**

```bash
# 1. Pull a dashboard to disk (creates ./resources/Dashboard/my-dashboard.yaml)
grafanactl resources pull dashboards/my-dashboard -p ./resources -o yaml

# 2. Edit the file (agent modifies spec fields, e.g. title, panels)
#    File is at: ./resources/Dashboard/my-dashboard.yaml

# 3. Validate before pushing
grafanactl resources validate -p ./resources -o json

# 4. Push changes back to Grafana
grafanactl resources push -p ./resources
```

Pull organizes files as `{Kind}/{Name}.{ext}` under the `-p` path:

```
./resources/
├── Dashboard/
│   └── my-dashboard.yaml
└── Folder/
    └── my-folder.yaml
```

**Export all resources for version control:**

```bash
# Pull everything, commit to git
grafanactl resources pull -p ./resources -o yaml
git add ./resources && git commit -m "snapshot Grafana resources"
```

## Environment Variables

**Authentication:**

| Variable | Description |
|----------|-------------|
| `GRAFANA_SERVER` | Target Grafana instance URL (required) |
| `GRAFANA_TOKEN` | Service account token or API key (recommended) |
| `GRAFANA_USER` | Username for basic auth (not recommended for automation) |
| `GRAFANA_PASSWORD` | Password for basic auth (not recommended for automation) |

**Namespace (organization/stack):**

| Variable | Description |
|----------|-------------|
| `GRAFANA_ORG_ID` | Organization ID (on-prem Grafana) |
| `GRAFANA_STACK_ID` | Stack ID (Grafana Cloud) |

**Configuration:**

| Variable | Description |
|----------|-------------|
| `GRAFANACTL_CONFIG` | Custom config file path (default: `~/.config/grafanactl/config.yaml`) |
| `GRAFANACTL_AUTO_APPROVE` | Auto-approve destructive operations (automatically enables `--force` on delete) |

**Global CLI flags (available on all commands):**

| Flag | Description |
|------|-------------|
| `--context` | Active context name (overrides `current-context` in config) |
| `--config` | Custom config file path |
| `--no-color` | Disable color output |
| `-v, --verbose` | Increase verbosity (up to 3 times: `-vvv`) |

**Example CI/CD setup:**

```bash
export GRAFANA_SERVER="https://prod.grafana.net"
export GRAFANA_TOKEN="${GRAFANA_SERVICE_ACCOUNT_TOKEN}"  # From CI secrets

grafanactl resources push -p ./dashboards
```

**Auto-approval for CI/CD:**

For non-interactive delete operations in CI/CD pipelines, use the `--yes` flag or `GRAFANACTL_AUTO_APPROVE` environment variable to automatically enable the `--force` flag:

```bash
# Using --yes flag
grafanactl resources delete dashboards --yes

# Using environment variable (recommended for CI/CD)
export GRAFANACTL_AUTO_APPROVE=1
grafanactl resources delete dashboards
```

**Note:** Auto-approval only affects delete operations. For push/pull operations with externally-managed resources (e.g., Terraform, GitSync), you must still explicitly pass `--include-managed`.

## Resource Selectors


grafanactl resources push -p ./dashboards
```

## Resource Selectors

Grafana CLI uses kubectl-style selectors to target resources:

```bash
# All of a kind
grafanactl resources get dashboards
grafanactl resources get folders

# Specific resource by name
grafanactl resources get dashboards/my-dashboard

# Multiple resources
grafanactl resources get dashboards/dash1,dash2,dash3

# Multiple kinds
grafanactl resources get dashboards/foo folders/bar

# Fully-qualified (with API version)
grafanactl resources get dashboards.v1alpha1.dashboard.grafana.app/foo
```

## Resource Format

Resources use a Kubernetes-style structure. When you `pull` resources, this is the format you get. When you `push`, this is the format expected.

**Dashboard (JSON):**

```json
{
  "apiVersion": "dashboard.grafana.app/v1",
  "kind": "Dashboard",
  "metadata": {
    "name": "my-dashboard",
    "namespace": "default"
  },
  "spec": {
    "title": "My Dashboard"
  }
}
```

**Folder (YAML):**

```yaml
apiVersion: folder.grafana.app/v1beta1
kind: Folder
metadata:
  name: my-folder
  namespace: default
spec:
  title: My Folder
```

| Field | Description |
|-------|-------------|
| `apiVersion` | API group and version (e.g., `dashboard.grafana.app/v1`) |
| `kind` | Resource type: `Dashboard`, `Folder`, etc. |
| `metadata.name` | Resource identifier (UID in Grafana) — this is the value you use in selectors (e.g., `dashboards/<name>`) |
| `metadata.namespace` | Organization or Stack ID (set automatically on push) |
| `spec` | The resource payload — this is what you edit when modifying a dashboard or folder |

**Resource relationships:** Dashboards can belong to folders via the `grafana.app/folder` annotation in `metadata.annotations`. When pushing, grafanactl automatically pushes folders before dashboards to satisfy dependencies.

Use `grafanactl resources list -o wide` to discover available `apiVersion` and `kind` values for your Grafana instance.

### Command Output Examples

**`resources list -o json`** — returns available resource types:

```json
[
  { "group": "dashboard.grafana.app", "version": "v1", "plural": "dashboards", "singular": "dashboard", "kind": "Dashboard" },
  { "group": "folder.grafana.app", "version": "v1beta1", "plural": "folders", "singular": "folder", "kind": "Folder" }
]
```

**`resources get dashboards -o json`** — multiple resources return an `items` wrapper:

```json
{
  "items": [
    {
      "apiVersion": "dashboard.grafana.app/v1",
      "kind": "Dashboard",
      "metadata": { "name": "my-dashboard", "namespace": "default" },
      "spec": { "title": "My Dashboard" }
    }
  ]
}
```

A single named resource (`get dashboards/my-dashboard -o json`) returns the object directly, without the `items` wrapper.

**`resources validate -o json`** — returns an array of failures:

```json
{ "failures": [{ "file": "./resources/dashboard.yaml", "error": "spec.title is required" }] }
```

**`push`, `pull`, `delete`** — return a summary line (not JSON):

```
✔ 3 resources pushed, 0 errors
```

## Exit Codes

Grafana CLI uses standard exit codes for scripting and CI/CD:

| Code | Meaning |
|------|---------|
| `0` | Success (or `--on-error ignore` mode) |
| `1` | Error — invalid arguments, resource not found, API failure |

**Error handling modes** (`--on-error` flag):

| Mode | Behavior | Exit Code |
|------|----------|-----------|
| `fail` (default) | Process all resources, exit 1 if any failed | 0 or 1 |
| `abort` | Stop on first error | 0 or 1 |
| `ignore` | Process all resources, always exit 0 | 0 |

**Error output format** (written to stderr):

```
Error: Invalid configuration
│
│ Missing required field: server
│
├─ Suggestions:
│
│ • Review your configuration: grafanactl config view
│ • Check the documentation for proper configuration format
│
└─
```

Errors include a summary, optional details, actionable suggestions, and documentation links. Parse the first line (`Error: ...`) for a machine-readable summary.

**Example usage in scripts:**

```bash
if ! grafanactl resources get dashboards -o json > dashboards.json 2>error.log; then
  echo "Failed to fetch dashboards (exit code: $?)"
  cat error.log
  exit 1
fi
```

## For CI/CD & Automation

**GitHub Actions example:**

```yaml
name: Deploy Grafana Dashboards
on:
  push:
    branches: [main]
    paths: ['dashboards/**']

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install grafanactl
        run: |
          curl -L https://github.com/grafana/grafanactl/releases/latest/download/grafanactl-Linux-x86_64 -o grafanactl
          chmod +x grafanactl
          sudo mv grafanactl /usr/local/bin/

      - name: Validate dashboards
        env:
          GRAFANA_SERVER: ${{ secrets.GRAFANA_PROD_URL }}
          GRAFANA_TOKEN: ${{ secrets.GRAFANA_PROD_TOKEN }}
        run: |
          grafanactl resources validate -p ./dashboards -o json

      - name: Deploy to production
        env:
          GRAFANA_SERVER: ${{ secrets.GRAFANA_PROD_URL }}
          GRAFANA_TOKEN: ${{ secrets.GRAFANA_PROD_TOKEN }}
        run: |
          grafanactl resources push -p ./dashboards --on-error abort
```

**Key automation patterns:**

- **Idempotency:** `grafanactl resources push` is idempotent — running the same command multiple times produces the same result. Safe for repeated CI/CD runs.
- **Dry-run:** Use `--dry-run` on `push` and `delete` to preview changes before applying.
- **Error control:** Use `--on-error abort` to fail fast, `--on-error ignore` to continue past errors.
- **Structured output:** Use `-o json` or `-o yaml` for machine-parseable output on `get`, `list`, `pull`, and `validate` commands.
- **Manager metadata:** Grafana CLI tracks which tool manages each resource. Use `--include-managed` to modify resources created by other tools (e.g., Grafana UI).
- **Concurrency:** Use `--max-concurrent` (default: 10) to control parallel API calls on `push`, `delete`, `validate`, and `serve` commands.

## Live Development Server

Grafana CLI includes a built-in development server with live reload:

```bash
# Serve resources from a directory
grafanactl resources serve ./resources

# With a generation script (e.g., grafana-foundation-sdk)
grafanactl resources serve --script 'go run ./dashboards/...' --script-format yaml

# Custom port
grafanactl resources serve ./resources --port 3001
```

The server provides:

- Reverse proxy to your Grafana instance
- Automatic live reload on file changes via WebSocket
- Dashboard preview in the browser
- Script execution for code-generated dashboards

> [!NOTE]
> The `kubernetesDashboards` feature toggle must be enabled in Grafana for `resources serve`.

## Claude Code Plugin

A Claude Code plugin is included under [`claude-plugin/`](claude-plugin/README.md).
It gives Claude deep knowledge of grafanactl — skills for debugging, datasource
exploration, dashboard management, and alert investigation, plus a specialist
`grafana-debugger` agent. See [`claude-plugin/README.md`](claude-plugin/README.md)
for installation instructions.

## Documentation

See [the full documentation](https://grafana.github.io/grafanactl/) for comprehensive guides on:

- [Installation](https://grafana.github.io/grafanactl/getting-started/install/)
- [Configuration](https://grafana.github.io/grafanactl/getting-started/configure/)
- [CLI Reference](https://grafana.github.io/grafanactl/reference/cli/)
- [Environment Variables](https://grafana.github.io/grafanactl/reference/environment-variables/)

## Maturity

> [!WARNING]
> **This project is currently *in public preview*, which means that it is still under active development.**
> Bugs and issues are handled solely by Engineering teams. On-call support or SLAs are not available.

Additional information can be found in [Release life cycle for Grafana Labs](https://grafana.com/docs/release-life-cycle/).

## Contributing

See our [contributing guide](CONTRIBUTING.md).

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
