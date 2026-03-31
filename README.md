# gcx — Grafana Cloud CLI

<p>
<a href="https://github.com/grafana/gcx/actions/workflows/ci.yaml"><img src="https://github.com/grafana/gcx/actions/workflows/ci.yaml/badge.svg?branch=main" alt="CI"></a>
<a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
<img src="https://img.shields.io/badge/status-public%20preview-orange" alt="Public Preview">
</p>

Grafana Cloud and the Grafana Assistant — in your terminal and your agentic coding environment.

Query production. Investigate alerts. Let the Assistant root-cause issues. Ship fixes with observability built in. Without leaving your editor.

*"Don't guess. Check the actual production data."*

## Why gcx

Agentic coding tools like Claude Code and Cursor have changed how developers build software. But they're flying blind. Code ships. Observability comes later — if it comes at all.

gcx closes that gap. It connects your editor to your entire Grafana Cloud production stack — including the Grafana Assistant — making observability a **development signal**, not an afterthought. When something breaks, the Assistant's investigation is already waiting: mitigations planned, context assembled, so you can act immediately.

- **Grafana Assistant integration** — automated root-cause analysis, investigation summaries, and remediation suggestions powered by the Assistant
- **Production-aware development** — query live metrics, logs, and traces without leaving your editor
- **AI agent native** — JSON/YAML output, structured errors, predictable exit codes. Agent mode auto-detected for Claude Code, Copilot, Cursor, and others
- **Full Grafana Cloud access** — dashboards, alerting, SLOs, Synthetic Monitoring, OnCall, K6, Fleet Management, Incidents, and more from a single CLI
- **GitOps & CI/CD** — pull resources to files, version in git, push back with full round-trip fidelity
- **Observability as code** — scaffold projects, import dashboards, lint with Rego rules, live-reload dev server
- **Multi-environment** — named contexts to switch between dev, staging, and production

> [!NOTE]
> **gcx requires Grafana 12 or above.** Older Grafana versions are not supported.

## Maturity

> [!WARNING]
> **This project is currently *in public preview*, which means that it is still under active development.**
> Bugs and issues are handled solely by Engineering teams. On-call support or SLAs are not available.

See [Release life cycle for Grafana Labs](https://grafana.com/docs/release-life-cycle/).

## The Agentic Workflow

Here's what it looks like when your coding agent has access to production:

**1. An alert fires** — P95 latency on the checkout service crosses the SLO threshold.

**2. The Assistant investigates** — Your coding agent calls the Grafana Assistant through gcx. The Assistant has already started its investigation — it traces the issue to a missing index on `customer_id` causing full table scans under load.

**3. It fixes the issue** — Drafts the migration, adds the index.

**4. It prevents recurrence** — Instruments the service with OpenTelemetry spans, sets up a Synthetic Monitoring check on the checkout flow, and creates an alert rule on query duration.

**5. It ships** — Opens a PR, tests pass, deploys to production. The alert resolves.

Investigation, fix, instrumentation, monitoring — without the developer ever leaving their editor. The Grafana Assistant provides the intelligence; gcx provides the interface. And because it all builds on everything you've already configured in Grafana Cloud — your dashboards, your alerts, your datasources — no other tool can give you this depth out of the box.

## Install

**Pre-built binary (Linux/macOS/Windows):**

```bash
curl -fL https://github.com/grafana/gcx/releases/latest/download/gcx-$(uname -s)-$(uname -m) -o gcx
chmod +x gcx && sudo mv gcx /usr/local/bin/
```

**Go install:**

```bash
go install github.com/grafana/gcx/cmd/gcx@latest
```

**Shell completion:**

```bash
gcx completion zsh > "${fpath[1]}/_gcx"   # zsh
gcx completion bash > /etc/bash_completion.d/gcx  # bash
```

**Verify:** `gcx --version`

### AI Agent Plugin

A [Claude Code plugin](claude-plugin/README.md) is included with skills for
managing dashboards, exploring datasources, investigating alerts with the
Grafana Assistant, and debugging with live observability data. Install it
alongside gcx to give your agent deep Grafana knowledge.

## Quick Start

### 1. Authenticate

**Service account token (recommended):**

```bash
gcx config set contexts.my-grafana.grafana.server https://your-instance.grafana.net
gcx config set contexts.my-grafana.grafana.token your-service-account-token
gcx config use-context my-grafana
```

Use a [Grafana service account token](https://grafana.com/docs/grafana/latest/administration/service-accounts/) with **Editor** or **Admin** role.

**Grafana Cloud products (SLO, Synth, OnCall, etc.):**

Grafana Cloud products require a [Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/) for API access. Set it in your context:

```bash
gcx config set contexts.my-grafana.cloud.token your-cloud-access-policy-token
gcx config set contexts.my-grafana.cloud.org your-org-slug
```

**Environment variables (recommended for CI/CD and agents):**

```bash
export GRAFANA_SERVER="https://your-instance.grafana.net"
export GRAFANA_TOKEN="your-service-account-token"
export GRAFANA_CLOUD_TOKEN="your-cloud-access-policy-token"
export GRAFANA_CLOUD_ORG="your-org-slug"
```

> [!NOTE]
> `gcx auth login` provides an experimental OAuth browser flow. Manual token setup above is the recommended approach for now. A consolidated authentication experience — including Grafana Assistant CLI integration — is coming soon.

**Verify:** `gcx config check`

### 2. Explore

```bash
# Grafana resources
gcx resources schemas                           # discover available resource types
gcx resources get dashboards                    # list all dashboards
gcx resources get folders                       # list all folders
gcx alert rules list                            # list alert rules

# Grafana Cloud products
gcx slo definitions list                        # list SLOs
gcx synth checks list                           # list synthetic monitoring checks
gcx oncall schedules list                       # list on-call schedules
gcx k6 load-tests list                          # list k6 load tests

# Query datasources
gcx datasources prometheus query 'rate(http_requests_total[5m])' --since 1h
gcx datasources loki query '{app="nginx"} |= "error"' --since 1h
```

## Grafana Cloud Products

gcx provides dedicated commands for each Grafana Cloud product:

| Product | Command | Examples |
|---------|---------|----------|
| **SLOs** | `gcx slo` | `slo definitions list`, `slo reports list` |
| **Synthetic Monitoring** | `gcx synth` | `synth checks list`, `synth probes list` |
| **OnCall** | `gcx oncall` | `oncall schedules list`, `oncall integrations list` |
| **Alerting** | `gcx alert` | `alert rules list`, `alert groups list` |
| **K6 Cloud** | `gcx k6` | `k6 load-tests list`, `k6 runs list` |
| **Fleet Management** | `gcx fleet` | `fleet pipelines list`, `fleet collectors list` |
| **IRM Incidents** | `gcx incidents` | `incidents list`, `incidents create -f incident.yaml` |
| **Knowledge Graph** | `gcx kg` | `kg status`, `kg search`, `kg entities show` |
| **Adaptive Telemetry** | `gcx adaptive` | `adaptive metrics recommendations show`, `adaptive logs patterns show` |

## Resource Management

Manage both Grafana-native resources (dashboards, folders) and Grafana Cloud resources from a single CLI:

```bash
# Pull dashboards and folders to local files
gcx resources pull dashboards -p ./resources -o yaml
gcx resources pull folders -p ./resources -o yaml

# Push local changes back to Grafana
gcx resources push -p ./resources

# Preview changes without applying
gcx resources push -p ./resources --dry-run

# Validate resources before pushing
gcx resources validate -p ./resources

# Edit a dashboard interactively (opens $EDITOR)
gcx resources edit dashboards/my-dashboard

# Delete a resource
gcx resources delete dashboards/my-dashboard
```

## Alerting & Datasource Queries

Inspect alerting rules and query datasources directly:

```bash
# Alert rules
gcx alert rules list
gcx alert groups list

# PromQL queries
gcx datasources prometheus query 'rate(http_requests_total[5m])' --since 1h
gcx datasources prometheus labels
gcx datasources prometheus metadata

# LogQL queries
gcx datasources loki query '{app="nginx"} |= "error"' --since 1h
gcx datasources loki labels
gcx datasources loki series
```

gcx also supports Pyroscope (profiling) and Tempo (traces) datasources.

## Observability as Code

gcx includes tools for managing Grafana resources as Go code using the [grafana-foundation-sdk](https://github.com/grafana/grafana-foundation-sdk):

```bash
# Scaffold a new project
gcx dev scaffold --project my-dashboards

# Import existing dashboards from Grafana as Go builder code
gcx dev import dashboards

# Live-reload dev server (preview dashboards in browser)
gcx dev serve ./resources

# Lint resources with built-in and custom Rego rules
gcx dev lint run -p ./resources
gcx dev lint rules                              # list available rules
gcx dev lint new --resource dashboard --name my-rule  # create custom rule

# Build and push
go run ./dashboards/... | gcx resources push -p -
```

## Raw API Access

For anything not covered by built-in commands, use the API passthrough:

```bash
gcx api /api/health
gcx api /api/datasources -o yaml
gcx api /api/dashboards/db -d @dashboard.json
gcx api /api/dashboards/uid/my-dashboard -X DELETE
```

## GitOps

Pull resources to files, version in git, push back:

```bash
# Pull all resources
gcx resources pull -p ./resources -o yaml

# Commit to git
git add ./resources && git commit -m "snapshot Grafana resources"

# Push changes from git to Grafana
gcx resources push -p ./resources
```

gcx push is idempotent — running it multiple times produces the same result. Folders are automatically pushed before dashboards to satisfy dependencies.

## CI/CD

```yaml
# .github/workflows/deploy-resources.yaml
name: Deploy Grafana Resources
on:
  push:
    branches: [main]
    paths: ['resources/**']

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install gcx
        run: |
          curl -fL https://github.com/grafana/gcx/releases/latest/download/gcx-Linux-x86_64 -o gcx
          chmod +x gcx && sudo mv gcx /usr/local/bin/

      - name: Deploy resources
        env:
          GRAFANA_SERVER: ${{ secrets.GRAFANA_PROD_URL }}
          GRAFANA_TOKEN: ${{ secrets.GRAFANA_PROD_TOKEN }}
        run: |
          gcx resources validate -p ./resources
          gcx resources push -p ./resources --on-error abort
```

- All commands except `edit` are non-interactive — safe for pipelines
- `--dry-run` on `push` and `delete` to preview changes
- `--on-error abort|fail|ignore` to control error behavior
- `-o json` or `-o yaml` for machine-parseable output

## Documentation

| Topic | Description |
|-------|-------------|
| [Installation](docs/installation.md) | Install gcx on macOS, Linux, and Windows |
| [Configuration](docs/configuration.md) | Contexts, authentication, environment variables |
| [Managing Resources](docs/guides/manage-resources.md) | Get, push, pull, delete, edit, validate |
| [Dashboards as Code](docs/guides/dashboards-as-code.md) | Dashboard-as-code workflow with live dev server |
| [Linting Resources](docs/guides/lint-resources.md) | Lint dashboards and alert rules with Rego policies |
| [CLI Reference](docs/reference/cli/) | Full command reference (auto-generated) |

## Contributing

See our [contributing guide](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
