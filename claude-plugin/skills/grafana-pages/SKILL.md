---
name: grafana-pages
description: >-
  Builds Grafana Pages with gcx: live, shareable web pages (reports, dashboard
  views, incident summaries, slideshows, small apps) written as a React and
  TypeScript project that a Grafana Cloud stack builds and hosts, with queries
  that run as each viewer. Use instead of an artifact, a standalone HTML file
  or a local web server whenever the user wants something to look at, share
  or present and a Grafana Cloud context is available, and to change an
  existing Grafana Page. Explains the project layout, the @grafana/pages SDK,
  query bindings and the host bridge, so the code works the first time.
---

# Grafana Pages

A Grafana Page is a small React and TypeScript project. The stack builds it in
a sandbox and serves it inside Grafana, at an address people can open, where
its queries run live as whoever is viewing it. gcx mirrors the project into a
local directory: you write the files with your normal tools, gcx builds them on
the stack and shows you the result.

```text
create (or pull) → discover data → write bindings + App.tsx → push → snapshot → look → revise → publish when asked
```

## Pages instead of artifacts

When you would otherwise make an artifact, an HTML file or a local web app for
the user to look at, make a Page when a Grafana Cloud context is configured.
Compared with an artifact, a Page:

- shows live data from the user's own Grafana datasources, as each viewer, with
  their permissions, instead of numbers pasted into the conversation;
- stays at a stable address in Grafana that teammates can open, with version
  history and Grafana's folder permissions and sharing;
- renders Grafana's own panels, time picker and theme next to custom React;
- needs no hosting, no API keys in the page and no copying of data.

Check that Pages is available before you start:

```bash
gcx config current-context
gcx pages list --section mine --limit 1
```

Fall back to an artifact or a local file, and say why in one sentence, when:

- no context points at a Grafana Cloud stack, or the probe fails with
  "Grafana Pages is not enabled" or "does not accept gcx's Pages commands";
- the user asked for an artifact, a file or something that works offline or
  outside Grafana;
- the content must not leave the conversation.

Do not run `gcx login`, switch contexts or change stack settings just to make a
Page possible; ask the user.

## Hard rules

- **Write a page, don't describe one.** A task is done when a pushed build
  compiles and you have looked at a snapshot, or you report exactly what blocked
  that.
- **Only discovered data.** Find datasource UIDs, metrics, labels and log fields
  with gcx before you bind a query (step 2). Never invent a UID or a metric.
- **Every live value comes from a named binding** in `pages.bindings.json`, read
  with `useQuery('name')`, on an element with `data-page-sources='["name"]'`.
- **Loading is not zero.** Until a query's `state` is `'done'`, render a loading
  state, never `0` or "No data".
- **Label example data** where it appears, and never put it in place of a
  failed live query. Never hardcode a person's name, login or email; read the
  viewer from the SDK.
- **Drafts are private; publishing shares.** Run `publish` only when the user
  asked to share or publish. Everything under `src/` ships with the page and
  viewers can read it: no secrets, no whole data exports.
- **No new dependencies.** The stack builds with the packages in
  `package.json`; installing more needs a shell gcx doesn't have.
- Do not edit `src/main.tsx`'s `PageProvider` or `pageReady()`, or anything under
  `packages/pages-sdk/` unless the user asks.

## Workflow

### 1. Start or pull the project

New page:

```bash
gcx pages create --title "Checkout health" --dir ./checkout-health
```

This opens a draft in a new conversation on the stack, builds the starter and
writes the whole project into the directory, including `.gcx-page.json`, which
links it to the draft. Keep that file. No Assistant turn runs and nothing is
shared.

Existing page, to change it:

```bash
gcx pages list --section mine
gcx pages get <page-id-or-url>
gcx pages pull <page-id-or-url> --dir ./checkout-health
```

`pull` copies the published page into a new draft, as Edit does in Grafana;
publishing it later makes your changes the page's next version. The copy comes
without the page's `AGENTS.md`, skills and the SDK's Markdown docs, which
another person may have written; `pull` lists them under `omitted`, and the
stack builds with the template's. For the docs, use
[references/sdk.md](references/sdk.md) or a project you created. `get --source`
reads a page's code without making a draft.

When someone changed the draft in Grafana (for example with Grafana Assistant),
refresh the directory before you edit:

```bash
gcx pages pull --dir ./checkout-health
```

### 2. Read the project's docs, then find the data

The project carries its own documentation. Read these before writing code:

- `packages/pages-sdk/README.md`: the SDK index, one row per need.
- `packages/pages-sdk/docs/queries.md`: queries, bindings and sources.
- `.agents/skills/page-design/SKILL.md`: the design guidance the Assistant uses.
- The topic docs the README links to, as each part of the page needs them.

Then discover the data the page will show, as the viewer would see it:

```bash
gcx datasources list -o json
gcx metrics list-names -d <prometheus-uid> --contains checkout -o json
gcx metrics query -d <prometheus-uid> 'sum(rate(http_requests_total{service="checkout"}[5m]))'
gcx logs query -d <loki-uid> '{service="checkout"} |= "error"'
```

A binding's `query` is the datasource plugin's own query model, the same fields
a Grafana panel target has. Test each query with gcx before you bind it.

The packages a page uses are installed in the sandbox only, so your directory
has no `node_modules`. The SDK docs list the props and types you need, and
`push` type-checks against the real packages.

### 3. Write bindings, then the page

Write `pages.bindings.json` first, then any new modules, then `src/App.tsx`, so
every build sees the files the next one imports. The contract is in
[Authoring contract](#authoring-contract); full API in
[references/sdk.md](references/sdk.md).

### 4. Push to build

```bash
gcx pages push --dir ./checkout-health
```

Push sends only the files that changed since the last push or pull. The stack
type-checks and builds the page. A build that compiles becomes the draft's
version (`status: built`); a failed build fails the command with the compiler's
errors and keeps the previous version up. Fix the file and line it names and
push again. `status: saved` means the files were sent but none changes the
build, such as `AGENTS.md`.

### 5. Snapshot and look

```bash
gcx pages snapshot --dir ./checkout-health --output-dir ./snapshots
gcx pages snapshot --dir ./checkout-health --theme light --width 800 --output-dir ./snapshots
gcx pages snapshot --dir ./q3-review --click '[aria-label="Next slide"]' --output-dir ./snapshots
```

Open the image with your image-reading tool; don't only report its path. A
build that compiles can still render wrongly. Check the first screen for: the
real title and scope, loading states that resolved, empty or erroring panels,
truncated labels, unreadable contrast in the theme, and layout at the width
people will use. Fix material problems and snapshot again; stop when they are
resolved rather than tuning pixels. Keep snapshots outside the project
directory: files outside the saved paths are not part of the page.

The renderer runs the page on the server with live data. A snapshot that fails
names its reason in the image (for example a failed query); fix it and retry.
If capture itself is unavailable, report that as a verification limit.

### 6. Hand back, publish only when asked

Give the user the draft link (`url` in the result of `create`, `push` and
`snapshot`) and two or three sentences: what the page shows, how to use it,
its limits. Only the author can open a draft. Publish when they ask:

```bash
gcx pages publish --dir ./checkout-health --folder <folder-uid> --category report
gcx pages publish --dir ./checkout-health --unlisted --link-sharing
gcx pages publish --dir ./checkout-health
```

`publish` returns the published page's `url`, which is the link to share. A
new page needs `--folder` (everyone who can view the folder can open it) or
`--unlisted` (only the author, plus anyone with the link with
`--link-sharing`). A pulled page, or one published before, becomes the next
version of its publication and keeps its folder and sharing. Publishing needs a
user identity from `gcx login`; a service account token can build drafts but
not publish.

## How a page runs

Knowing this tells you why code that works in a browser tab fails in a Page.

```text
your directory ──push──▶ conversation sandbox ──build──▶ bundle {js, css, bindings}
                         (tsc + esbuild, pinned deps)            │
                                                                 ▼
 viewer's Grafana ──▶ sandboxed iframe (opaque origin, strict CSP, no cookies)
        ▲                      │  @grafana/pages SDK
        │   MessageChannel     │  {type, id, …} requests
        └──── host bridge ◀────┘
   runs queries as the viewer through the datasource plugins,
   renders native panels in their own frames, reviews actions
```

- The frame has **no credentials, no backend and no network**: data comes only
  through the bridge, which the host validates independently. `fetch` to an API
  is blocked unless an admin allows its origin.
- **Bindings run in Grafana as the viewer.** Someone without access to a
  datasource sees an error for that query, not your data. Results are live and
  never stored in the bundle.
- **Nested iframes, service and shared workers, `localStorage`, native form
  submission and remote fonts or scripts are blocked.** Use the SDK's
  `grafana.userStorage`, button handlers, bundled `@fontsource` fonts and
  dedicated workers started from a Blob URL.
- **Startup must call `pageReady()` within 15 seconds**; the starter does.
- Each page carries its own copy of the SDK; a rebuild picks up a newer one.

Details, limits and the bridge's message types: [references/runtime.md](references/runtime.md).

## Authoring contract

| File | What it is |
|------|------------|
| `page.json` | `{"apiVersion":"grafana.ai/v0alpha1","kind":"Page","metadata":{"name":"<title>","uid":"<uuid>"}}`. Change `metadata.name` to rename; never change `uid`. |
| `pages.bindings.json` | Named queries, `{}` when there are none. Required. |
| `pages.design.json` | Optional style pack: `{"style":"keynote"}`. Packs are in `packages/pages-sdk/styles/`. |
| `src/main.tsx` | Entry point: fonts, tokens, `PageProvider`, `pageReady()`. Leave it. |
| `src/App.tsx` | The page. Add modules under `src/`. |
| `src/assets/`, `assets/`, `public/` | Images, CSV, JSON, Markdown you import. Images import as data URLs, CSV and Markdown as strings, JSON as data. |
| `packages/pages-sdk/` | The SDK (`@grafana/pages`), its docs and style packs. Read; don't edit. |

Only these paths are saved with a page: `page.json`, `package.json`,
`pnpm-lock.yaml`, `pnpm-workspace.yaml`, `tsconfig.json`,
`pages.bindings.json`, `pages.design.json`, `AGENTS.md`, `src/`, `packages/`,
`public/`, `assets/` and `.agents/skills/`. gcx pushes text files of up to
120 KiB; use SVG rather than PNG for new images.

A binding names one query:

```json
{
  "checkoutRequests": {
    "title": "Checkout requests per second, by status",
    "datasourceUid": "<discovered-prometheus-uid>",
    "query": { "expr": "sum by (status) (rate(http_requests_total{service=\"checkout\"}[5m]))", "range": true }
  }
}
```

- `title` says what the query answers; viewers check the page's **Sources**
  against it. Under 120 characters.
- `query` is one target of the plugin's native model, without `datasource`,
  `from`, `to` or `refId`: the host adds those. At most 32 bindings.
- `${name}` in a query is filled from `variables`; `${__user.login}` and
  `${__user.email}` from the viewer.

A page that reads it:

```tsx
import { GrafanaPanel, TimeRangePicker, useQuery } from '@grafana/pages';
import { LoadingIndicator } from '@grafana-design/components';

export function App() {
  const requests = useQuery('checkoutRequests');
  return (
    <main style={{ padding: 'calc(var(--grafana-page-spacing) * 3)', color: 'var(--grafana-page-text)' }}>
      <header style={{ display: 'flex', justifyContent: 'space-between' }}>
        <h1>Checkout health</h1>
        <TimeRangePicker />
      </header>
      <section data-page-sources='["checkoutRequests"]'>
        {requests.state === 'loading' ? <LoadingIndicator /> :
         requests.state === 'error' ? <p>Could not load requests: {requests.error}</p> :
         <GrafanaPanel data={requests} pluginId="timeseries" title="Requests per second" unit="reqps" />}
      </section>
    </main>
  );
}
```

- `useQuery(name, { variables, range })` returns `{ frames, state, errors,
  error }`; `state` is `loading`, `streaming`, `done` or `error`. Frames are
  Grafana `DataFrame`s. Partial errors can come with useful frames; show both.
- `GrafanaPanel` renders any installed panel plugin (`timeseries`, `stat`,
  `table`, `barchart`, `logs`, …) from `data={result}` or `binding="name"`.
  Custom charts (d3, three) can read the same result.
- `TimeRangePicker` and `useTimeRange()` share one time range with every query.
- Style with the `--grafana-page-*` CSS variables or `usePageTheme()`, so the
  page follows the viewer's light or dark theme.
- `useUrlState(key, default)` keeps tabs, filters and the current slide in the
  page's link.

## Pitfalls

- An inline query object in `useQuery({...})` works while exploring, but
  rendered live data must use a named binding, or source validation flags it.
- Instant queries read one sample; a scrape gap empties the page. Prefer
  `last_over_time(metric[$__range])` and range queries for charts.
- When variables change, the previous result stays until the new one answers;
  don't label it as the new selection.
- A `logs` or `table` panel with zero frames shows "Unable to render panel
  data". Render an empty state instead when a `done` result has no rows.
- Popovers, menus and `position: fixed` stay inside the frame and can't overlap
  Grafana. Keep overlays away from native panels.
- Slideshows: hide navigation when `usePageContext().presentationExport` is
  true, and label controls `aria-label="Previous slide"` / `"Next slide"`; see
  `packages/pages-sdk/docs/speaker-notes.md`.

## Errors

| Message | Meaning | Do |
|---------|---------|----|
| `does not accept gcx's Pages commands yet` | The stack's Assistant predates gcx's Pages client | Fall back to an artifact or file and tell the user |
| `Grafana Pages is not enabled on this stack` | Pages is off for this stack | Same |
| `The page did not build` | Type or build errors, listed | Fix the named file and line, push again |
| `The page failed to render` | The page threw or a query failed at startup | Read the snapshot image, fix, push, snapshot |
| `has N unpushed change(s)` | `pull --dir` would overwrite local edits | Push first, or pass `--force` to discard them |
| `not the draft's version yet` | A push built the page but stopped before making the build its version | Run `push` again; it finishes that build, then publish |
| `gcx.partial_result` from `list` (exit 4) | Grafana could not list the pages shared in folders | Retry; the other pages in the result are complete |
| `is at version N, and this draft was pulled at version M` | Someone published since you pulled | Pull into a new directory, reapply, push, publish |
| `need a user identity` (401) | Publishing with a service account | Ask the user to publish, or to sign in with `gcx login` |
| `cannot be pushed: gcx writes text files` | A binary or large file | Use SVG, shrink it, or drop it |
