# gcx — Grafana Cloud CLI

<p>
<a href="https://github.com/grafana/gcx/actions/workflows/ci.yaml"><img src="https://github.com/grafana/gcx/actions/workflows/ci.yaml/badge.svg?branch=main" alt="CI"></a>
<a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License"></a>
<img src="https://img.shields.io/badge/status-public%20preview-orange" alt="Public Preview">
</p>

![gcx](./gcx.png)

Grafana Cloud and the Grafana Assistant — in your terminal and your agentic coding environment.

Query production. Investigate alerts. Let the Assistant root-cause issues. Ship fixes with observability built in. Without leaving your editor.

*"Don't guess. Check the actual production data."*

## Why gcx

GCX brings the full power of Grafana Cloud and Grafana Assistant to your command line. It bridges the gap between your local environment and key observability insights from Grafana Cloud.

Since the end of 2025, adoption of agentic coding tools like Cursor and Claude Code have exploded. You are coding faster than ever before. Your agents see your code — but they are blind to your production environment. They don't see the latency spikes, the server load, or whether you're actually hitting your SLOs. They write code based on what could happen, not what is actually happening. This is a dangerous gap.

We built GCX to close that gap.

> [!NOTE]
> gcx supports Grafana Cloud, Enterprise, and OSS, see the [compatibility matrix](#compatibility) for details on what is and isn't supported across different Grafana products.
> **Grafana 12 or above is required.** Older Grafana versions are not supported.

## Quick Start

### 1. Install

**Quick install (Linux/macOS):**

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | sh
```

Downloads the latest release, verifies the SHA-256 checksum, and installs to
`~/.local/bin`. Override the location with `INSTALL_DIR`:

```sh
curl -fsSL https://raw.githubusercontent.com/grafana/gcx/main/scripts/install.sh | INSTALL_DIR=/usr/local/bin sh
```

**Homebrew (macOS and Linux):**

```bash
brew install grafana/grafana/gcx
```

Compiles from source on your machine (requires Homebrew's `go`, installed
automatically as a build dependency). First install takes ~30–60 seconds
while Go fetches dependencies; subsequent upgrades are faster.

**Pre-built binary (Linux/macOS/Windows):**

Download the latest archive for your OS and architecture from the
[releases page](https://github.com/grafana/gcx/releases/latest),
extract it, and move the binary to your PATH:

```bash
tar xzf gcx_*.tar.gz
chmod +x gcx && sudo mv gcx /usr/local/bin/
```

On macOS, the manually-downloaded binary may be blocked on first run with
*"Apple could not verify…"* or `killed: 9` — see
[macOS Gatekeeper and killed: 9](docs/installation.md#macos-gatekeeper-and-killed-9)
for the one-time workaround. The `curl | sh` installer above handles this
automatically.

**Go install:**

```bash
go install github.com/grafana/gcx/cmd/gcx@latest
```

**Shell completion:**

```bash
gcx completion zsh > "${fpath[1]}/_gcx"   # zsh
gcx completion bash > /etc/bash_completion.d/gcx  # bash
gcx completion fish > ~/.config/fish/completions/gcx.fish  # fish
```

**Verify:** `gcx --version`


### 2. Authenticate

`gcx login` creates or re-authenticates a context. It auto-detects whether the server is Grafana Cloud (`*.grafana.net`) or on-premises and adjusts the prompt accordingly. Pick the path below that matches your setup.

**Grafana Cloud, browser-based OAuth (interactive, recommended):**

```bash
gcx login my-stack --server https://my-stack.grafana.net
```

Opens a browser for OAuth, then saves the access token, refresh token, and proxy endpoint to the `my-stack` context and makes it current. Best for day-to-day use on Cloud stacks. If OAuth doesn't suit your setup, pick "Service account token" at the prompt.

**Service account token (Cloud or on-premises, recommended for CI/automation):**

```bash
gcx login my-grafana --server https://your-instance.grafana.net --token glsa_xxx --yes
```

Use a [Grafana service account token](https://grafana.com/docs/grafana/latest/administration/service-accounts/) with **Editor** or **Admin** role. Works for both Cloud and on-premises; this is the only auth method available for on-premises instances.

**Grafana Cloud product APIs (SLO, Synthetic Monitoring, IRM, etc.):**

Cloud product commands require a [Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/) in addition to Grafana auth. Provide it at login:

```bash
gcx login my-stack --server https://my-stack.grafana.net --token glsa_xxx --cloud-token glc_xxx --yes
```

Or add it later by re-running `gcx login` against the same context:

```bash
gcx login --context my-stack   # prompts for the Cloud Access Policy token; Enter to skip
```

`gcx` derives the Cloud stack slug from `--server` when possible. Set it explicitly only for custom domains where gcx cannot derive it:

```bash
gcx config set contexts.my-stack.cloud.stack your-stack-slug
```

You do not need to set `cloud.api-url` for `grafana.com`; gcx defaults to `https://grafana.com`. Set `cloud.api-url` only when you need a non-default Grafana Cloud API endpoint.

**Environment variables (CI/CD, agents):**

```bash
export GRAFANA_SERVER="https://your-instance.grafana.net"
export GRAFANA_TOKEN="your-service-account-token"
export GRAFANA_CLOUD_TOKEN="your-cloud-access-policy-token"

# Optional: only needed if gcx cannot derive the stack slug from GRAFANA_SERVER.
export GRAFANA_CLOUD_STACK="your-stack-slug"
```

Env vars resolve at every command invocation, so you can run `gcx` commands directly without a prior `gcx login`.

**Verify:** `gcx config check`

See the [login reference](docs/reference/login.md) for the full guide, including re-authentication, environment-variable setup, and troubleshooting for common errors.

### 3. See It in Action

**Query production from your terminal:**

```sh
$ gcx metrics query 'rate(http_requests_total{service="checkout"}[5m])' --since 1h
SERVICE     TIMESTAMP             VALUE
checkout    2026-04-15 09:00:00   142.3
checkout    2026-04-15 09:05:00   151.7
checkout    2026-04-15 09:10:00   289.4
checkout    2026-04-15 09:15:00   312.1
```

**Check what's firing:**

```sh
$ gcx alert rules list --state firing
UID     NAME                            STATE    HEALTH   PAUSED
abc1    Checkout P95 > SLO threshold    firing   ok       no
def2    Disk usage > 85%                firing   ok       no
```

**Review SLO status:**

```sh
$ gcx slo definitions list
UUID    NAME                        TARGET   WINDOW   STATUS
uid1    Checkout Availability       99.90%   30d      ok
uid2    API Latency P99 < 200ms     99.50%   30d      at_risk
uid3    Payment Processing          99.95%   30d      breaching
```

**Visualize metrics directly in your terminal:**

```sh
$ gcx metrics query 'topk(6, sum by (container) (rate(container_cpu_usage_seconds_total{namespace="ditl-demo-prod", container!="", container!="POD"}[5m])))' --since 6h -o graph
```

![Terminal graph output](./graph_example.png)

**Explore more**

```bash
# Grafana resources
gcx resources schemas                           # discover available resource types
gcx resources get dashboards                    # list all dashboards
gcx resources get folders                       # list all folders
gcx alert rules list                            # list alert rules

# Grafana Cloud products
gcx synth checks list                           # list synthetic monitoring checks
gcx irm oncall schedules list                   # list on-call schedules
gcx k6 load-tests list                          # list k6 load tests

# Query more datasources
gcx logs query '{app="nginx"} |= "error"' --since 1h
gcx traces query '{.cluster="dev-us-central-0"}' --since 1h
```

### 4. Install Agent Skills

gcx ships a portable Agent Skills bundle for setup, dashboard GitOps,
datasource exploration, alert investigation, structured debugging, SLO
management, Synthetic Monitoring workflows, project scaffolding, resource
generation and import, and end-to-end observability rollout.

**For Claude Code**

Use the dedicated [Claude Code plugin](claude-plugin/README.md):

```text
/plugin marketplace add grafana/gcx
/plugin install gcx@gcx-marketplace
```

**For other `.agents`-compatible harnesses**

For example: OpenAI Codex, OpenCode, and Pi. View the skills shipped in the bundle with:

```sh
gcx skills list
18 skill(s) bundled with gcx

SKILL                      INSTALLED    DESCRIPTION
explore-datasources        yes          Discover what datasources, metrics, labels, and log streams are available in a Grafana instance.
gcx-observability          yes          (Experimental) End-to-end observability setup for Grafana Cloud.
....
```

Install the bundle into `~/.agents/skills` with:
```sh
gcx skills install --all
```

## The Agentic Workflow

Here's what it looks like when your coding agent has access to production:

**1. An alert fires** — P95 latency on the checkout service crosses the SLO threshold.

**2. The Assistant investigates** — Your coding agent calls the Grafana Assistant through gcx. The Assistant has already started its investigation — it traces the issue to a missing index on `customer_id` causing full table scans under load.

**3. It fixes the issue** — Drafts the migration, adds the index.

**4. It prevents recurrence** — Instruments the service with OpenTelemetry spans, sets up a Synthetic Monitoring check on the checkout flow, and creates an alert rule on query duration.

**5. It ships** — Opens a PR, tests pass, deploys to production. The alert resolves.

Investigation, fix, instrumentation, monitoring — without the developer ever leaving their editor. The Grafana Assistant provides the intelligence; gcx provides the interface. And because it all builds on everything you've already configured in Grafana Cloud — your dashboards, your alerts, your datasources — no other tool can give you this depth out of the box.

```sh
$ gcx assistant investigations list
ID    TITLE                                     STATUS     UPDATED
abc1  Checkout P95 latency breach               active     2m ago
def2  Memory leak in payment-svc                resolved   1h ago
```

## Compatibility

gcx works across Grafana's product offerings. Feature availability depends on your deployment:

| Feature | OSS (12+) | Enterprise (12+) | Cloud | BYOC |
|---------|:---------:|:----------------:|:-----:|:----:|
| Resource management (dashboards, folders) | ✓ | ✓ | ✓ | ✓ |
| Alert rules | ✓ | ✓ | ✓ | ✓ |
| Raw API passthrough (`gcx api`) | ✓ | ✓ | ✓ | ✓ |
| Observability as Code (`gcx dev`) | ✓ | ✓ | ✓ | ✓ |
| Signal queries (metrics, logs, traces, profiles) | ✓ † | ✓ † | ✓ | ✓ |
| SLO, Synthetic Monitoring, IRM, k6, Fleet, etc. | ✗ | ✗ | ✓ | ◐ |
| Adaptive Metrics / Logs / Traces | ✗ | ✗ | ✓ | ◐ |
| Grafana Assistant | ✗ | ✗ | ✓ | ✗ |

**† Self-hosted signal queries** — `gcx metrics query`, `gcx logs query`, `gcx traces query`, and `gcx profiles query` work against self-hosted datasources (Prometheus, Loki, Tempo, Pyroscope), but datasource endpoints must be configured manually. For Grafana Cloud, endpoints are auto-discovered from your stack.

**◐ BYOC** — Bring Your Own Cloud runs the Grafana stack on your own infrastructure while connecting to the Grafana Cloud control plane. Core Grafana features (dashboards, alerts, signal queries) work in full. Cloud product availability (SLO, Synthetic Monitoring, IRM, etc.) depends on which plugins are installed and configured in your BYOC stack.

## Maturity

> [!WARNING]
> **Public preview** — gcx is under active development. Bugs are handled by Engineering; on-call support and SLAs are not available. See [release life cycle](https://grafana.com/docs/release-life-cycle/).

## Grafana Cloud Products

gcx provides dedicated commands for each Grafana Cloud product:

| Product | Command | Examples |
|---------|---------|----------|
| **SLOs** | `gcx slo` | `slo definitions list`, `slo reports list` |
| **Synthetic Monitoring** | `gcx synth` | `synth checks list`, `synth probes list` |
| **IRM** | `gcx irm` | `irm oncall schedules list`, `irm oncall integrations list`, `irm incidents list`, `irm incidents create -f incident.yaml` |
| **Alerting** | `gcx alert` | `alert rules list`, `alert groups list` |
| **k6 Cloud** | `gcx k6` | `k6 load-tests list`, `k6 runs list` |
| **Fleet Management** | `gcx fleet` | `fleet pipelines list`, `fleet collectors list` |
| **Knowledge Graph** | `gcx kg` | `kg status`, `kg search`, `kg entities show` |
| **Frontend Observability** | `gcx frontend` | `frontend apps list`, `frontend apps get` |
| **App Observability** | `gcx appo11y` | `appo11y overrides get`, `appo11y settings get` |
| **Sigil (AI Observability)** | `gcx sigil` | `sigil conversations list`, `sigil agents list`, `sigil rules list` |
| **Assistant** | `gcx assistant` | `assistant prompt`, `assistant investigations list`, `assistant investigations report` |
| **Adaptive Metrics** | `gcx metrics adaptive` | `metrics adaptive recommendations show`, `metrics adaptive rules list` |
| **Adaptive Logs** | `gcx logs adaptive` | `logs adaptive patterns show`, `logs adaptive drop-rules list` |
| **Adaptive Traces** | `gcx traces adaptive` | `traces adaptive recommendations show`, `traces adaptive policies list` |
| **Profiles (Pyroscope)** | `gcx profiles` | `profiles query`, `profiles labels` |
| **Traces (Tempo)** | `gcx traces` | `traces query`, `traces get`, `traces labels` |

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
gcx metrics query 'rate(http_requests_total[5m])' --since 1h
gcx metrics labels
gcx metrics metadata

# LogQL queries
gcx logs query '{app="nginx"} |= "error"' --since 1h
gcx logs labels
gcx logs series --match '{app="nginx"}'
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
          curl -fL "$(curl -s https://api.github.com/repos/grafana/gcx/releases/latest | grep browser_download_url | grep linux_amd64.tar.gz | cut -d '"' -f 4)" | tar xz gcx
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
