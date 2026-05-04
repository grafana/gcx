# synthetic-monitoring-datasource support gap

`gcx datasources query <uid> probes` fails with:

```
Datasource type "synthetic-monitoring-datasource" is not supported (supported prometheus, loki, pyroscope)
```

## Where the limitation lives

### 1. The error — `cmd/gcx/datasources/query.go:157–159`

The auto-detect `datasources query` handler switches on the normalized
datasource type. `synthetic-monitoring-datasource` hits the `default` case:

```go
default:
    return fmt.Errorf("datasource type %q is not supported (supported: prometheus, loki, pyroscope)", dsType)
```

### 2. The switch — `cmd/gcx/datasources/query.go:75–159`

Only three arms exist:

| Case | Lines |
|------|-------|
| `"prometheus"` | 76– |
| `"loki"` | 100– |
| `"pyroscope"` | 128– |

Tempo is also normalized by `NormalizeKind()` but has no case here.

### 3. Type normalization — `internal/datasources/query/resolve.go:114–123`

`NormalizeKind()` maps plugin IDs to short names
(`grafana-pyroscope-datasource` → `pyroscope`, etc.).
`synthetic-monitoring-datasource` is not mapped and passes through unchanged.

## How existing datasource queries reach Grafana

All three clients are built with `rest.HTTPClientFor(&cfg.Config)` from
`k8s.io/client-go`, which picks up bearer token / basic auth / TLS from the
named context (`internal/config/rest.go:276–287`) and wraps the transport with
logging and retry layers.

**Prometheus and Loki** (`internal/query/prometheus/client.go`,
`internal/query/loki/client.go`) use the same endpoint:

```
POST /apis/query.grafana.app/v0alpha1/namespaces/{namespace}/query
```

with a fallback to `POST /api/ds/query` on 404. The datasource UID goes in
the request body:

```json
{
  "queries": [{ "refId": "A", "datasource": { "type": "prometheus", "uid": "<uid>" }, "expr": "..." }],
  "from": "...",
  "to": "..."
}
```

**Pyroscope** (`internal/query/pyroscope/client.go`) uses the datasource proxy
with the UID in the URL and query parameters in the body:

```
POST /api/datasources/proxy/uid/{datasourceUID}/{resourcePath}
```

e.g. `.../querier.v1.QuerierService/SelectMergeStacktraces`

The proxy approach lets each plugin define its own sub-routes. The SM
datasource follows this same pattern.

### SM datasource proxy routes

Route prefixes are defined in
`src/datasource/plugin.json` of the sm-app repository. Grafana injects the
`accessToken` from `secureJsonData` automatically.

| Proxy path prefix | Forwards to |
|---|---|
| `viewer-token` | `{apiHost}/api/v1/register/viewer-token` |
| `save` | `{apiHost}/api/v1/register/save` |
| `sm/` | `{apiHost}/api/v1/` (catch-all) |
| `api/v1alpha1/secrets` | `{apiHost}/api/v1alpha1/secrets` |

Concrete paths called by `src/datasource/DataSource.ts`:

| Resource | Method | Path |
|---|---|---|
| Probes | GET | `sm/probe/list` |
| | POST | `sm/probe/add` |
| | POST | `sm/probe/update` |
| | DELETE | `sm/probe/delete/{id}` |
| Checks | GET | `sm/check/list` |
| | GET | `sm/checks/info` |
| | POST | `sm/check/add` |
| | POST | `sm/check/update` |
| | POST | `sm/check/update/bulk` |
| | POST | `sm/check/adhoc` |
| | DELETE | `sm/check/delete/{id}` |
| | GET/PUT | `sm/check/{id}/alerts` |
| Tenant | GET | `sm/tenant` |
| | GET | `sm/tenant/limits` |
| | GET | `sm/tenant/cals` |
| | GET | `sm/tenant/settings` |
| | POST | `sm/tenant/settings/update` |
| Tokens | POST | `sm/token/create` |
| Registration | POST | `sm/register/save` |
| Channels | GET | `sm/channels/k6` |
| Secrets | GET/POST | `api/v1alpha1/secrets` |
| | GET/PUT/DELETE | `api/v1alpha1/secrets/{name}` |

There is no query language involved — these are plain REST calls. For
`gcx datasources query <uid> probes`, the natural mapping is
`GET sm/probe/list`.

## Why adding support is non-trivial

Unlike Prometheus, Loki, and Pyroscope, the SM datasource is not a
time-series query endpoint. It exposes entity tables (probes, checks, etc.)
through the Grafana datasource proxy using a plugin-specific request format
that is not PromQL or LogQL.

The SM HTTP client already exists in `internal/providers/synth/` but it talks
to the SM API directly, bypassing the Grafana datasource proxy entirely.

## Fix options

**Option A — Add SM to the auto-detect query dispatcher**

1. Research the SM datasource proxy query API (or reuse `internal/providers/synth/`).
2. Add a `"synthetic-monitoring-datasource"` case to the switch in
   `cmd/gcx/datasources/query.go`.
3. Optionally add a `NormalizeKind()` mapping.
4. Register an SM `DatasourceProvider` in
   `internal/datasources/providers/syntheticmonitoring.go`.

**Option B — Surface SM data through the provider tier**

`gcx synth probes list` (or similar) via the existing SM provider may already
exist or be easier to build, avoiding the need to shoehorn a table query into
the time-series query pipeline.
