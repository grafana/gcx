# Fleet Management through the collector app plugin proxy

**Created**: 2026-08-31
**Status**: accepted
**Supersedes**: none

## Context

Fleet Management was the only gcx product that needed a grafana.com Cloud Access
Policy token. `internal/fleet/config.go` read `AgentManagementInstanceURL` and
`AgentManagementInstanceID` from the grafana.com stack record, then called the
Fleet Management API directly with Basic authentication
(`{instanceID}:{apiToken}`).

That design forced three costs on a user:

- A `gcx login` to the stack was not enough. A user also needed
  `gcx cloud login`, or a Cloud Access Policy token.
- The token needed the `fleet-management:read` and `fleet-management:write`
  scopes. gcx requested both for every user, whether or not they used Fleet.
- Every fleet command paid for a grafana.com stack lookup, even the commands
  that never read the stack record.

The Grafana Cloud `grafana-collector-app` plugin already proxies the same API
for its own user interface. It declares classic app proxy routes and injects the
credentials server-side.

Measurements against a live stack on 2026-08-31, plugin version 4.27.0:

- `/api/plugin-proxy/grafana-collector-app/fleet-management-api/<rpc>` serves
  every remote procedure call that gcx uses.
- `plugin.json` names 17 read routes at the Viewer level. Every other path
  matches the wildcard route, which needs the Admin role or the action
  `grafana-collector-app:admin`.
- `GET /api/plugin-proxy/grafana-collector-app/grafanacom-api/instances/`
  returns the grafana.com instance record for the stack. It decodes into
  `cloud.StackInfo` field for field, so the instrumentation commands can derive
  their backend URLs and Prometheus headers without a grafana.com token.
- `/api/plugins/grafana-collector-app/resources/...` does not work. The plugin
  has no backend. Only the plugin proxy works.

## Decision

Fleet Management and the Instrumentation Hub reach their API only through the
collector app plugin proxy at `cfg.Host`. There is no fallback to the direct
API.

- `internal/fleet.Client` carries no credentials. It takes an `*http.Client`
  built with `rest.HTTPClientFor` from the stack REST config.
- `internal/fleet.LoadClient` and `LoadClientWithStack` take a loader with
  `LoadGrafanaConfig`, not `LoadCloudConfig`.
- `LoadClientWithStack` reads `cloud.StackInfo` through the
  `grafanacom-api/instances/` proxy route. The result is memoized per Grafana
  host.
- gcx no longer requests the `fleet-management:read` and
  `fleet-management:write` scopes from grafana.com.
- `gcx setup status` reports the plugin state and the two plugin actions, so a
  user can diagnose the cause before a command fails.

Rejected: keep both transports, with the plugin proxy as a fallback. Two
transports mean two authorization models and two error surfaces for the same
commands. The direct path also carries the cost that this decision removes.

## Consequences

Easier:

- A stack login alone drives `gcx fleet` and `gcx instrumentation`.
- gcx asks grafana.com for two fewer scopes.
- `LoadClient` performs no stack lookup at all. It used to pay for one and
  discard the result.
- The proxy holds the Fleet Management credential, so gcx never handles it.

Harder:

- gcx now depends on the plugin. A stack without the `grafana-collector-app`
  plugin cannot run these commands. Self-hosted Grafana never could, because
  Fleet Management is a Grafana Cloud product.
- Every write needs the Admin role or the `grafana-collector-app:admin` action.
  A Viewer can read only.
- `plugin.json` is not a stable contract. A plugin release can change the route
  set. `gcx setup status` and the typed error for a missing plugin route limit
  the cost of that risk. Grafana returns more than one body for that cause, so
  `fleet.IsPluginMissingBody` matches a set of markers without regard to case.
- A 404 now has two meanings. `internal/providers/fleet/client.go` tests the
  body before it reports a missing resource.

Follow-up: none required. `providers.CloudRESTConfig`, `LoadCloudConfig`, and
`cloud.GCOMClient` stay, because k6, Synthetic Monitoring, the Faro sourcemap
upload, Adaptive telemetry, `gcx cloud stacks`, and the login validation still
use them.
