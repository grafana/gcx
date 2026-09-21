---
title: Diagnose missing telemetry
---

# Diagnose missing telemetry with gcx

Use gcx and its existing agent skills to investigate an empty dashboard or
missing application telemetry. Start with evidence, ask before repairs, and
verify the original symptom afterward. You do not need a new diagnostic skill
or Grafana Cloud. An agent harness and its model access are separate from gcx.

## Before you start

- Follow [Install gcx](../sources/installation.md) if `gcx version` is unavailable.
- Follow [Install Agent Skills](https://github.com/grafana/gcx#install-agent-skills) for your
  harness. Start a session that can discover the installed skills. The existing
  `debug-with-grafana` skill handles investigation; `setup-gcx` helps with setup.
- Use a reachable Grafana instance supported by your gcx version, with access
  to the relevant datasources. A static instrumentation check alone does not
  provide a telemetry destination.
- If you have no backend, [start docker-otel-lgtm](https://github.com/grafana/docker-otel-lgtm#run-the-docker-image)
  for a local development sink. Setting up a sink does not authorize changing
  an existing application's exporter. A local proof does not establish that
  your production destination works.
- Know which application or fixture you may inspect. Do not enumerate unrelated
  containers, environments, or services to compensate for missing context.

The command examples use the gcx 1.3.0 command surface. Check `gcx version` and
the relevant command's `--help` when following them with another release.

## Connect without changing your usual context

For an already configured destination, use explicit `--config` and `--context`
arguments. Do not switch the current context just for this investigation.
Otherwise, create a separate private configuration outside your checkout:

```bash
GCX_DIAGNOSTICS_DIR=$(mktemp -d)
GCX_DIAGNOSTICS_CONFIG="$GCX_DIAGNOSTICS_DIR/config.yaml"
(umask 077; printf '{}\n' > "$GCX_DIAGNOSTICS_CONFIG")
```

Keep the path until cleanup. In another terminal, set the variable to the same
path; do not create a second empty configuration by repeating the block.

For an existing Grafana destination, use interactive login with a new context
in that file (replace the example URL):

```bash
gcx login diagnostics --config "$GCX_DIAGNOSTICS_CONFIG" \
  --server https://your-grafana.example
```

For local LGTM, use that repository's local connection instructions instead of
this login step. See [Configure gcx](../sources/configuration.md) for supported
authentication methods. Supply credentials locally through the supported auth
flow, not in chat, copied transcripts, or committed files. Prefer read-only
access when available; approval instructions do not restrict API permissions.

Environment overrides can still affect the selected configuration. Review
which overrides you intentionally set before running queries; do not dump your
whole environment or configuration into a transcript. Do not disable TLS
verification to make a connection error disappear.

Use your selected context name below (`diagnostics` is the login example):

```bash
gcx config check --config "$GCX_DIAGNOSTICS_CONFIG" --context diagnostics
gcx datasources list --config "$GCX_DIAGNOSTICS_CONFIG" --context diagnostics
```

Stop and resolve connection/authentication errors before interpreting query
results. A successful configuration check does not prove application ingestion.
Choose the datasource UID returned by discovery, rather than assuming a default.
For example, against a Prometheus datasource:

```bash
gcx metrics query 'vector(1)' --datasource 'PROMETHEUS_DATASOURCE_UID' \
  --config "$GCX_DIAGNOSTICS_CONFIG" --context diagnostics
```

Replace `PROMETHEUS_DATASOURCE_UID` before running. A returned value proves that query
path works, not that the application emitted metrics. A successful empty query,
an invalid query, and a failed connection are different observations.

## Give the agent the symptom and boundaries

Start the agent in the relevant application/test checkout. Provide:

- The original dashboard URL, failed test assertion, or checker finding.
- The explicit gcx config path, context, and intended destination (no secrets).
- The application/service identity and one representative operation, if known.
- The UTC time window, observed query, and request/trace identifiers, if available.
- The relevant Compose project, deployment, or Collector configuration you own.
- Which resources may be inspected and which actions require approval.

Unknown fields are not a reason to invent answers. Ask the agent to identify
what information or access is missing. Use this prompt, filling in what you know:

> Use the existing gcx skills to investigate `<symptom>`. Query only
> `<config path and context>` and inspect only `<application/fixture scope>`.
> The expected operation is `<operation>` in `<time window>`.
> Diagnose first and distinguish observed evidence from hypotheses. State what
> you could not observe; do not infer data loss from missing access.
> Ask before changing application, Collector, dashboard, or test configuration,
> restarting resources, or enabling payload logging. Do not weaken an assertion
> to make a test pass. Propose the smallest justified repair, and stop for approval.

An empty dashboard can result from no data being created, export failure,
filtering, an incorrect query, or a time/identity mismatch. Static configuration
and absence of results are leads, not proof of a particular failure boundary.
Ask for the evidence supporting each conclusion and the next discriminating check.

## Verify a repair and restore the environment

After approving a specific change:

1. Exercise a fresh representative operation and record its time and available
   identifiers. Old telemetry must not satisfy the recovery check.
2. Query the relevant datasource, then recheck the original dashboard or rerun
   the original test uncached. A successful backend query alone does not prove
   the panel or assertion is correct.
3. Review the actual diff and runtime changes. Functional recovery does not
   prove unrelated dashboard/configuration fields were preserved.
4. Restore temporary diagnostic configuration and logging. Remove temporary
   backups and stop only resources created for this investigation, after confirming
   they are no longer needed. Never clean up pre-existing resources by assumption.
5. Record the confirmed cause, remaining unknowns, approved changes, and result.

Debug-exporter payloads and logs may contain sensitive data. Enable them only
with permission, for a bounded investigation, and do not publish raw output.
Avoid full environment dumps or HTTP payload logging that can expose credentials.

If you created the private directory above, inspect it locally and remove only
that directory when finished. Remove any diagnostic credentials stored by login
using your normal credential-management process; deleting a config file does
not revoke a server-side token. Explicit config/context arguments leave your
usual current context unchanged. Do not delete an existing config used for the
investigation, or revoke a credential shared with another workflow.
