# Contributing Guidelines

This document is a guide to help you through the process of contributing to `gcx`.

Before implementing features or commands, read:

- [ARCHITECTURE.md](ARCHITECTURE.md) — system architecture, pipeline diagrams, ADR index
- [DESIGN.md](DESIGN.md) — CLI UX design: command grammar, output model, taste rules
- [CONSTITUTION.md](CONSTITUTION.md) — invariants you must not violate
- [docs/design/](docs/design/) — prescriptive UX implementation rules (output, errors, agent mode, naming, …)

## Issue Tracking

Issues are tracked in [GitHub Issues](https://github.com/grafana/gcx/issues).
Use the issue templates when creating new issues — they set the correct issue
type and labels automatically.

### Issue types

GitHub's native issue types classify issues. Don't add type prefixes to titles.

| Type | When to use |
|------|-------------|
| **Bug** | Something is broken or behaving unexpectedly |
| **Task** | A specific piece of implementation work |
| **Feature** | New functionality or capability |
| **Enhancement** | Improvement to existing functionality |
| **Epic** | Large effort spanning multiple issues |

### Issue title convention

Write clear, concise titles. The style depends on the issue type:

| Type | Style | Good | Bad |
|------|-------|------|-----|
| Task / Feature | **Imperative verb** | "Add OnCall provider" | "OnCall provider" |
| Enhancement | **Imperative verb** | "Improve cold-start latency" | "[Enhancement]: cold start is slow" |
| Bug | **Descriptive symptom** | "Excessive warnings for unconfigured resources" | "[Bug]: warnings" |
| Epic | **Noun phrase (scope)** | "OAuth authentication via Grafana Assistant" | "Epic: do OAuth stuff" |

Rules:
- No type prefixes (`[Bug]:`, `Epic:`, `[Feature]:`) — the issue type field handles this
- Start with a capital letter
- Be specific — someone should understand the scope from the title alone
- Tasks and features start with a verb: Add, Implement, Port, Create, Fix, Improve, etc.

### Labels

| Prefix | Purpose |
|--------|---------|
| `area/` | Codebase area (providers, cli-ux, core, skills, docs) |
| `priority/` | Severity (critical, high, medium, low, none) |
| `action/` | Workflow state (needs-triage) |

### Milestones

Issues are grouped into milestones representing release targets. Check the
[milestones page](https://github.com/grafana/gcx/milestones) for current targets.

## Development environment

`gcx` relies on [`devbox`](https://www.jetify.com/devbox/docs/) to manage all
the tools required to work on it.

A shell including all these tools is accessible via:

```console
$ devbox shell
```

This shell can be exited like any other shell, with `exit` or `CTRL+D`.

One-off commands can be executed within the devbox shell as well:

```console
$ devbox run go version
```

Packages can be installed using:

```console
$ devbox add go@1.24
```

Available packages can be found on the [NixOS package repository](https://search.nixos.org/packages).

## Testing against a real Grafana API

While unit tests are valuable for testing individual components, integration testing against a real Grafana instance is important to ensure `gcx` works correctly with the actual Grafana API.

### Quick Start

The repository includes a `docker-compose.yml` file that sets up a complete test environment with:

- **Grafana 12.2** (latest stable release)
- **MySQL 8.0** (as the backend database)
- Pre-configured with `admin:admin` credentials
- The `kubernetesDashboards` feature toggle enabled (required for `gcx`)

### Starting the test environment

Start the services using the Make target:

```console
$ make test-env-up
```

This will start both Grafana and MySQL, wait for them to be healthy, and display the connection information.

You can also start the services manually:

```console
$ docker-compose up -d
```

Check the status of the services:

```console
$ make test-env-status
```

Or manually:

```console
$ docker-compose ps
```

You should see both `gcx-grafana` and `gcx-mysql` in a `healthy` state.

Verify Grafana is accessible:

```console
$ curl -u admin:admin http://localhost:3000/api/health
```

You should receive a JSON response indicating Grafana is running.

### Testing with gcx

The repository includes a pre-configured test config file at `testdata/integration-test-config.yaml` that you can use to test `gcx` against the local Grafana instance.

#### View the test configuration

```console
$ devbox run go run ./cmd/gcx --config testdata/integration-test-config.yaml config view
```

#### List available resources

```console
$ devbox run go run ./cmd/gcx --config testdata/integration-test-config.yaml resources schemas
```

#### Create a test dashboard

1. Create a dashboard YAML file (e.g., `test-dashboard.yaml`):

```yaml
apiVersion: v1alpha1
kind: Dashboard
metadata:
  name: test-dashboard
spec:
  title: Test Dashboard
  tags: [test]
  timezone: browser
  schemaVersion: 36
```

2. Push it to Grafana:

```console
$ devbox run go run ./cmd/gcx --config testdata/integration-test-config.yaml resources push test-dashboard.yaml
```

3. Pull it back to verify:

```console
$ devbox run go run ./cmd/gcx --config testdata/integration-test-config.yaml resources get dashboards/test-dashboard
```

#### Testing the serve command

The `serve` command allows you to develop dashboards locally with live reload:

```console
$ devbox run go run ./cmd/gcx --config testdata/integration-test-config.yaml dev serve test-dashboard.yaml
```

Then open your browser to the URL shown in the output (typically `http://localhost:8080`).

### Stopping the test environment

When you're done testing, stop the services:

```console
$ make test-env-down
```

Or manually:

```console
$ docker-compose down
```

To remove all data (including database volumes):

```console
$ make test-env-clean
```

Or manually:

```console
$ docker-compose down -v
```

### Customizing the test environment

#### Modifying Grafana configuration

The Grafana instance uses a custom configuration file at `testdata/grafana.ini`. You can modify this file to change Grafana's behavior. After making changes, restart the services:

```console
$ docker-compose restart grafana
```

#### Using a different Grafana version

To test against a different Grafana version, modify the `image` field in `docker-compose.yml`:

```yaml
services:
  grafana:
    image: grafana/grafana:12.1  # or any other version
```

Then restart the services:

```console
$ docker-compose up -d --force-recreate grafana
```

#### Viewing logs

To view logs from both services:

```console
$ make test-env-logs
```

To view logs from a specific service:

```console
$ docker-compose logs -f grafana
```

Or for MySQL:

```console
$ docker-compose logs -f mysql
```

### Troubleshooting

#### Grafana won't start or is unhealthy

Check the logs for errors:

```console
$ docker-compose logs grafana
```

Common issues:
- MySQL not fully initialized yet - wait a few more seconds and check again
- Port 3000 already in use - stop any other Grafana instances or change the port in `docker-compose.yml`

#### Cannot connect to Grafana from gcx

Verify Grafana is accessible:

```console
$ curl -u admin:admin http://localhost:3000/api/health
```

If this fails, check:
- Services are running: `docker-compose ps`
- Firewall settings are not blocking port 3000
- Check Grafana logs: `docker-compose logs grafana`

#### Database connection errors

Check MySQL is healthy:

```console
$ docker-compose ps mysql
```

If MySQL is not healthy, check the logs:

```console
$ docker-compose logs mysql
```

You may need to remove the volume and recreate it:

```console
$ docker-compose down -v
$ docker-compose up -d
```
