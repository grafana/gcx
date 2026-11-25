# Data in a Grafana Page: bindings and queries

How a page gets live data, from a binding in `pages.bindings.json` to the frames
a component renders. The project's `packages/pages-sdk/docs/queries.md` is the
reference for the SDK the page builds with; when the two disagree, the project
wins.

## Contents

- [The path of a query](#the-path-of-a-query)
- [Bindings](#bindings)
- [Query models](#query-models)
- [Variables and the time range](#variables-and-the-time-range)
- [Results and their lifecycle](#results-and-their-lifecycle)
- [Reading frames](#reading-frames)
- [Panels from the same data](#panels-from-the-same-data)
- [Sources](#sources)
- [Limits](#limits)
- [Data from outside Grafana](#data-from-outside-grafana)
- [Symptoms and causes](#symptoms-and-causes)

## The path of a query

```text
useQuery('requests')
  → SDK sends query-subscribe { binding: 'requests', variables, range, background } over the bridge
  → host looks up 'requests' in the page's bundle (own keys only)
  → host builds { datasource: binding.datasourceUid, targets: [{ ...binding.query, refId: 'A' }], variables }
  → an isolated query frame runs it through the datasource plugin, as the viewer
  → host streams query-result / query-complete / query-error back
  → useQuery updates { frames, state, errors, error, range, request }
```

- The bindings are compiled into the bundle at build time. A page can only run
  the bindings its own bundle declares; a name it does not declare fails with
  "This query binding is not declared by the page."
- The datasource plugin runs in Grafana's isolated query frame with the
  viewer's session, including the plugin's frontend query logic. No
  credentials, token or HTTP proxy ever reach the page's frame.
- The datasource's permissions and configuration decide what a query can do,
  including what a SQL query may change. Two viewers can get different results.
- Results are live and never stored with the page.

## Bindings

`pages.bindings.json` maps a name to one query:

```json
{
  "checkoutErrors": {
    "title": "Checkout requests answered with a 5xx, per second",
    "datasourceUid": "<discovered-prometheus-uid>",
    "query": { "expr": "sum(rate(http_requests_total{service=\"checkout\",status=~\"5..\"}[5m]))", "range": true }
  },
  "checkoutRequests": {
    "title": "Checkout requests per second",
    "datasourceUid": "<discovered-prometheus-uid>",
    "query": { "expr": "sum(rate(http_requests_total{service=\"checkout\"}[5m]))", "range": true }
  }
}
```

- **`title`** says what the query answers: the population, the measure and the
  grouping, under 120 characters. Viewers read it in the page's **Sources**,
  next to the query and its result, to check the page measures what it claims.
- **`datasourceUid`** is a UID you discovered with `gcx datasources list`.
  Never guess one; a dashboard datasource is rejected.
- **`query`** is one target in the plugin's own model, the fields a Grafana
  panel target has. Leave out `datasource`, `datasourceId`, `datasourceUid`,
  `from` and `to`: the host adds them. The host sets `refId` to `A`.
- The build checks: at most 32 bindings, names up to 80 characters, UIDs up to
  128, titles up to 200, and each `query` an object of at most 64 KiB.
- Give queries that answer different questions their own binding and title,
  even when they read the same datasource.

## Query models

| Datasource | Typical `query` |
|------------|-----------------|
| Prometheus, Mimir | `{ "expr": "…", "range": true }` for a chart, `{ "expr": "…", "instant": true }` for one value |
| Loki | `{ "expr": "{service=\"checkout\"} \|= \"error\"", "queryType": "range" }` |
| Tempo | `{ "query": "{ status = error }", "queryType": "traceql" }` |
| MySQL, Postgres | `{ "rawSql": "SELECT … WHERE $__timeFilter(created_at)", "format": "table" }` |
| Infinity | The plugin's `type`, `source`, `url`, `parser` fields, as in a panel |

Run each query with gcx before you bind it, against the same datasource:

```bash
gcx metrics query -d <prometheus-uid> 'sum(rate(http_requests_total{service="checkout"}[5m]))'
gcx logs query -d <loki-uid> '{service="checkout"} |= "error"'
gcx traces query -d <tempo-uid> '{ status = error }'
```

## Variables and the time range

- `useQuery('name', { variables: { region: 'eu' } })` passes variables to the
  query. They become Grafana scoped variables, so the plugin interpolates them
  itself: `${region}`, `${region:sqlstring}`, `${services:csv}`.
- `${__user.login}`, `${__user.email}` and `${__user.id}` are the viewer.
- Every query runs over the page's shared time range, which `TimeRangePicker`
  and `useTimeRange()` change; all queries refresh together. Pages start on the
  last hour; call `setRange` once on mount for another default.
- Pass `range` in the options to fix one query's interval. A range is
  `{ from, to }` of strings: relative (`now-6h`, `now/d`), ISO 8601, or epoch
  milliseconds. `$__range`, `$__interval` and `$__timeFilter` follow it.
- `maxDataPoints` in an inline request sets the resolution.

## Results and their lifecycle

`useQuery` returns `{ frames, state, errors, error?, range?, request?, traceIds? }`.

| `state` | Meaning |
|---------|---------|
| `loading` | No answer yet. `frames` is empty: show a loading state, never 0 or "No data" |
| `streaming` | Partial or live results are arriving; `range` advances for queries ending at `now` |
| `done` | The query finished. Empty `frames` now really means no data |
| `error` | `error` is the first message; `errors` lists each target's status |

- Previous frames stay while a query refreshes or fails, so show an error and a
  retry next to the old data rather than replacing it.
- Partial errors can come with useful frames: show both.
- A query that does not answer within 45 seconds fails. Hidden browser tabs
  pause queries, keep their results and resume them on return; pass
  `{ background: true }` to keep one running.
- Identical requests share one subscription and a small cache, across hooks and
  plain JavaScript. A range change or refresh updates them all.
- `useQuery` cancels on unmount and when its inputs change. For one-shot reads,
  such as an export, `await grafana.query(request, { signal })`; for custom
  charts that update in place, `grafana.subscribe(request, onResult)` returns a
  stop function. Rendered data should still use `useQuery`, so the page's
  source validation can see it.
- Inline requests, `{ title, datasource: '<uid>', targets: [{ refId: 'A', … }] }`,
  are for exploring. Rendered live data needs a named binding.

## Reading frames

`frames` are Grafana `DataFrame`s: `{ name?, length, fields: [{ name, type,
values, labels?, config? }] }`. A numeric series normally has a `time` field and
a `number` field, with its labels on the number field. Read by type, never by
position alone, and handle empty results:

```tsx
const errors = useQuery('checkoutErrors');
const requests = useQuery('checkoutRequests');
const last = (r: typeof errors) =>
  r.frames[0]?.fields.find((f) => f.type === 'number')?.values.at(-1) as number | undefined;

if (errors.state === 'loading' || requests.state === 'loading') return <LoadingIndicator />;
const rate = last(errors) !== undefined && last(requests) ? last(errors)! / last(requests)! : undefined;
return <strong data-page-sources='["checkoutErrors","checkoutRequests"]'>
  {rate === undefined ? 'No requests in range' : `${(rate * 100).toFixed(2)}% errors`}
</strong>;
```

Labels, field config, nulls, numbers and nanosecond timestamps survive
transport; functions do not.

## Panels from the same data

`GrafanaPanel` renders any installed panel plugin over a slot in the page,
inside its own isolated frame:

- `<GrafanaPanel data={result} pluginId="timeseries" />` renders a `useQuery`
  result without running the query again. Pass the whole result, so datasource
  links keep their context.
- `<GrafanaPanel binding="name" pluginId="stat" />` runs the binding itself and
  annotates its own sources. Passing both `data` and `binding` throws.
- Computed data uses the same shape, `{ frames, state, errors, range? }`; wrap
  that panel in an element whose `data-page-sources` lists every input.
- `options`, `fieldConfig` and `transformations` are the plugin's own. Save the
  plugin version they were written for in `pluginVersion`.

## Sources

Viewers can select any value and open **Source** to see the query behind it.
That needs `data-page-sources` with the binding names on the element that
shows live data, or on a shared container such as a chart or table:

- List every binding a derived value uses, such as
  `'["checkoutErrors","checkoutRequests"]'` for an error rate.
- Override a child whose sources differ; never annotate the page root or static
  text.
- At most 50 names, 512 characters each, 8,000 characters in total, and every
  name must be in `pages.bindings.json`.

Snapshots check this after queries settle and warn about live data without an
annotation, inline requests rendering live data and unknown names.

## Limits

| What | Limit |
|------|-------|
| Bindings | 32; each `query` up to 64 KiB |
| Inline request | 32 targets with unique `refId`s, `maxDataPoints` up to 5,000, 256 KiB |
| Running queries | 8 at once, 64 more queued in order |
| One result | 32 targets, 1,000 frames, 100,000 cells, 4 MiB |
| Waiting | 45 s for a first answer; 30 s without data stops a query; streams end after 5 minutes |
| Snapshot | About 25 s for the whole capture: keep the queries a page needs on load light |

## Data from outside Grafana

A page cannot `fetch` an API. Data Grafana does not hold, such as tickets or
CRM records, comes through the viewer's MCP connections in Grafana:
`useMcpTool(server, tool, args)` and `grafana.mcp.call`, with one review per
server for reads and one per write. Writes to an HTTP API go through an
Infinity datasource with `grafana.actions.http`, which the viewer approves each
time. See the project's `docs/mcp.md` and `docs/actions.md`.

## Symptoms and causes

| Symptom | Likely cause |
|---------|--------------|
| "This query binding is not declared by the page" | `useQuery('name')` without that key in `pages.bindings.json`, or a push that hasn't built since |
| A count shows 0 while the page loads | Rendered before `state` was `done` |
| The value is empty after a scrape gap | An instant query reads one sample; use `last_over_time(metric[$__range])` or a range query |
| A panel briefly shows the previous selection | The last result stays while new variables load; track which variables the frames belong to |
| A `logs` or `table` panel says "Unable to render panel data" | A `done` result with no frames; render an empty state instead |
| Works in the snapshot, empty for a colleague | Their datasource permissions: queries run as each viewer |
| "Grafana request timed out" | No first answer within 45 s; narrow the query or the range |
| Snapshot warns about sources | Live data without `data-page-sources`, an inline request on screen, or an unknown name |
