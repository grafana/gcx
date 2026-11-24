# @grafana/pages SDK reference

A map of the SDK a page imports from `@grafana/pages`, so you know what exists
before you open a topic doc. The authoritative docs and source are in every
project: `packages/pages-sdk/README.md`, `packages/pages-sdk/docs/*.md` and
`packages/pages-sdk/src/index.ts`. When this file and the project disagree, the
project wins: it is the SDK the page builds with.

## Contents

- [Entry point](#entry-point)
- [Data](#data)
- [Sources and validation](#sources-and-validation)
- [Panels and controls](#panels-and-controls)
- [Theme, styles and fonts](#theme-styles-and-fonts)
- [State, viewer and navigation](#state-viewer-and-navigation)
- [Assistant, actions and external systems](#assistant-actions-and-external-systems)
- [Installed packages](#installed-packages)

## Entry point

`src/main.tsx` mounts the app inside `PageProvider` and calls `pageReady()`
once the first render commits. `PageProvider` supplies the host theme,
frame-local portals for menus and tooltips, and the bridge connection. Keep
both; a page that has not called `pageReady()` within 15 seconds fails to start.

## Data

| API | Use |
|-----|-----|
| `useQuery(bindingOrRequest, { variables?, range?, background? })` | Rendered data. Returns `{ frames, state, errors, error?, range?, request? }`. |
| `grafana.query(request, { signal? })` | One-shot read in plain JavaScript, such as an export. |
| `grafana.subscribe(request, onResult, opts?)` | Streaming or custom-chart updates; returns a stop function. |
| `grafana.datasources.list()` | `{ uid, name, type }[]` the viewer can query. |
| `grafana.refresh()` | Rerun every query. |

A named binding in `pages.bindings.json`:

```json
{
  "<name>": {
    "title": "What this query answers",
    "datasourceUid": "<uid>",
    "query": { "...the plugin's own target fields": "..." }
  }
}
```

Limits the host enforces: 32 bindings; names up to 80 characters; a `query`
object up to 64 KiB and without `datasource`, `datasourceId`, `datasourceUid`,
`from` or `to`. A binding the page uses but doesn't declare fails with "This
query binding is not declared by the page."

An inline request, for exploring: `{ title, datasource: '<uid>', targets: [{
refId: 'A', ...target }], variables?, maxDataPoints? }`, up to 32 targets.

Per result: 1,000 frames, 100,000 cells, 4 MiB. The host runs eight queries at
once and queues 64 more; a query that sends nothing for 30 seconds is stopped,
and streams end after five minutes. Hidden browser tabs pause queries unless
`background: true`.

Query models by datasource, as in a panel target:

| Datasource | Typical `query` |
|------------|-----------------|
| Prometheus / Mimir | `{ "expr": "…", "range": true }` or `{ "expr": "…", "instant": true }` |
| Loki | `{ "expr": "{service=\"checkout\"} \|= \"error\"", "queryType": "range" }` |
| Tempo | `{ "query": "{ status = error }", "queryType": "traceql" }` |
| SQL (MySQL, Postgres) | `{ "rawSql": "SELECT … WHERE $__timeFilter(created_at)", "format": "table" }` |

Variables become Grafana scoped variables, so each plugin interpolates them
natively: `${region}`, `${region:sqlstring}`, `${services:csv}`. The viewer is
`${__user.login}`, `${__user.email}`, `${__user.id}`.

## Sources and validation

Viewers can select any value on a page and open **Source** to see the query
behind it. That works only when the element carries the binding names:

```tsx
<span data-page-sources='["errors","requests"]'>{errorRate}</span>
```

- Put it on the element or a shared container (a chart, a table) showing live
  data; list every binding a derived value uses. A bound `GrafanaPanel`
  annotates itself.
- Never put a blanket annotation on the page root, or tags on static text.
- At most 50 names, 512 characters each, 8,000 characters in total.

Snapshots validate this after queries settle and warn about live data without
an annotation, inline queries rendering live data, and names missing from
`pages.bindings.json`.

## Panels and controls

| API | Use |
|-----|-----|
| `GrafanaPanel({ data \| binding, pluginId, title?, height?, unit?, decimals?, options?, fieldConfig?, transformations?, pluginVersion?, onEvent? })` | Any installed panel plugin. Pass `data={result}` to share a query with custom React. |
| `getPanelCatalog()` | Installed panel types and whether each is supported. |
| `TimeRangePicker({ hideText?, quickRanges? })` | Grafana's own picker, wired to every query. |
| `useTimeRange()` | `{ range, timezone, setRange, setTimezone, refresh }`. Pages start on the last hour; call `setRange` once on mount for another default. |

Native panels render in their own isolated frames, at most 32 at once, up to
4096×2400 pixels each. Keep the whole query result when transforming it, so
datasource links keep their context.

`@grafana/ui` (13.2.2) supplies `Button`, `Input`, `Select`, `Combobox`,
`Switch`, `Modal`, `Field` and the rest. `@grafana-design/components` adds
`LoadingIndicator`, `Pill`, `IconButton`, `Tooltip`, `Popover`,
`SectionHeader`, `IconTag`, `IconCallout`, `ThinkingIndicator`. Icons come from
`@grafana-design/icons`. Their type declarations live in the sandbox's
`node_modules`, not in your directory: read `packages/pages-sdk/docs/components.md`
and `docs/design/*.md` for props, and let push type-check.

## Theme, styles and fonts

- CSS variables, following the viewer's light or dark theme:
  `--grafana-page-{background, surface, surface-secondary, surface-elevated,
  text, text-secondary, text-disabled, link, border, border-strong, primary,
  primary-text, success, warning, error, font-family, font-monospace, font-size,
  font-weight, radius, spacing}`.
- `usePageTheme()` returns Grafana's theme object (`theme.spacing(2)`,
  `theme.colors.text.primary`, `theme.visualization.getColorByName`).
- Style packs: `pages.design.json` `{"style": "<id>"}` with one of
  `packages/pages-sdk/styles/<id>/`, such as `grafana-native`, `keynote`,
  `newsroom`, `developer-minimal`, `field-guide`. `usePageStyle()` returns its
  tokens; packs add `--style-*` variables. Read `docs/styles.md` first.
- Fonts must be bundled: `import '@fontsource/<family>/latin-600'`. Inter and
  JetBrains Mono are loaded by the starter; about 28 `@fontsource` families are
  installed (see `docs/theme.md`). Google Fonts and other remote fonts are blocked.

## State, viewer and navigation

| API | Use |
|-----|-----|
| `useUrlState(key, default, { description?, values? })` | Tab, filter, selection or slide in the page's link. Keys `^[\w.-]{1,64}$`; all state under 2,000 URL-encoded characters. |
| `usePageContext()` | `{ range, colorMode, timezone, locale, viewer?, presentationExport?, visit? }`. |
| `usePageContext().viewer` | `{ login, name, email? }` of the person viewing. |
| `grafana.userStorage.getItem / setItem / deleteItem` | One viewer's preferences; string values up to 64 KiB. Not `localStorage`. |
| `useLastVisit()`, `rangeEndingAt(range, since)` | Mark what changed since the viewer last looked. |
| `grafana.navigate(pathOrDestination)` | Go to a dashboard (`{ kind: 'dashboard', uid }`), Explore (`'/explore?…'`), an IRM alert group, another page. |
| `grafana.downloadCsv({ filename, headers, rows })`, `grafana.downloadFile(...)` | Exports, each confirmed by the viewer. |
| `useSpeakerNotes(...)`, `DemoDeck`, `CapturedView` | Slideshows and product demos; see `docs/speaker-notes.md`, `docs/product-demos.md`. |

Links: plain `<a href>` works. Grafana routes open directly; other sites ask the
viewer first.

## Assistant, actions and external systems

| API | Use |
|-----|-----|
| `useInlineAssistant()` | Generate a short interpretation inside the page. One generation per page at a time. |
| `openAssistant({ prompt, context? })` | Open Grafana Assistant with page context. Add only when asked. |
| `usePageTool(name, definition)` | Let Assistant read the page or drive it (next slide). At most 12. |
| `grafana.actions.http({ datasourceUid, method, url, body? })` | A reviewed write through an Infinity datasource. Each call is confirmed by the viewer. |
| `grafana.actions.slack({ text })` | A reviewed Slack DM, when `grafana.actions.isAvailable('slack.notify')`. |
| `useMcpTool(server, tool, args)`, `grafana.mcp.call(...)` | Data from systems outside Grafana through the viewer's MCP connections; see `docs/mcp.md`. |

Nothing that acts as the viewer runs while the page is hidden, is an unaccepted
build, or is being captured.

## Installed packages

Use only what `package.json` lists; gcx cannot install packages.

- React and React DOM 19.3.0, `@emotion/css`, `@emotion/react`.
- `@grafana/ui`, `@grafana/data`, `@grafana/runtime`, `@grafana/i18n` 13.2.2,
  kept together.
- `@grafana-design/components`, `design-tokens`, `icons`, `theme-providers`.
- `d3-array`, `d3-geo`, `d3-hierarchy`, `d3-sankey`, `d3-scale`, `d3-shape`,
  `three`, `topojson-client`, `world-atlas`, `html-to-image`.
- About 28 `@fontsource/*` families.
