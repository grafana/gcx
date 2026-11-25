# How a Grafana Page is built and run

What happens between `gcx pages drafts push` and a viewer seeing the page,
so you can tell a code problem from a platform rule. The SDK hides the bridge:
pages call SDK functions, never `postMessage` themselves. How queries and
bindings work is in [data.md](data.md).

## Contents

- [Build](#build)
- [Release and versions](#release-and-versions)
- [The frame](#the-frame)
- [The bridge](#the-bridge)
- [Security model](#security-model)
- [Limits](#limits)
- [Symptoms and causes](#symptoms-and-causes)

## Build

Each draft lives in a conversation on the stack, with its own sandbox VM. The
project sits at `/workspace/pages/<project>/` there, and the directory gcx
writes locally is a mirror of it.

1. `push` writes each changed file into the sandbox.
2. The sandbox installs the pinned dependencies offline (`pnpm install
   --offline --frozen-lockfile`), so a package missing from `package.json`
   can't be added.
3. It runs the TypeScript check and esbuild together. esbuild produces one
   minified script and one stylesheet, and inlines imported assets (images and
   fonts as data URLs, CSV and Markdown as text, JSON as data).
4. The output is a bundle, `{ version: 1, javascript, css, bindings, design? }`.
   It holds no HTML you wrote and no query results.

Every file write builds, so gcx writes data and modules before the
`src/App.tsx` that imports them. A failed type check or build keeps the last
working version and returns the errors, which `push` prints.

## Release and versions

A build that compiles becomes a **candidate**. gcx accepts it, which makes it
the draft's **release**: the version Grafana shows for the draft and that
`snapshot` captures. `publish` copies a release into the page catalog as a
**publication**, which has its own numbered versions and history. People
viewing the publication keep seeing its last published version until you
publish again.

## The frame

Grafana renders a page in an `<iframe sandbox="allow-scripts">` without
`allow-same-origin`, so the page gets an opaque origin. It has no Grafana
cookies, session or `localStorage`, and no way to reach Grafana's DOM. A
wrapper document creates the page's own frame and forwards a single
`MessageChannel` port to it.

The page document's Content-Security-Policy:

| Directive | Effect |
|-----------|--------|
| `default-src 'none'` | Nothing loads unless allowed below |
| `script-src` | Inline (the bundle) and a few CDNs; no `eval` |
| `connect-src` | Grafana's origin and origins an admin allowed; other `fetch` and WebSocket calls fail |
| `img-src`, `font-src` | `data:` and `blob:`, plus approved HTTPS for images; remote fonts fail |
| `frame-src 'none'` | No nested iframes or embeds |
| `worker-src blob:` | Dedicated workers from a Blob URL only; no shared or service workers |
| `form-action 'none'` | Native form submission does nothing |
| `base-uri 'none'` | No `<base>` |

An admin sets the stack's allowed origins under Pages network access, and an
author can request extra origins for one page in Grafana. Data from Grafana
never needs them: it comes through the bridge.

## The bridge

After the frame loads, the host posts `{ type: 'grafana-pages:init', version:
1, context }` with one port. The SDK accepts it only from its parent and only
once. Everything after that travels over the port.

- Requests: `{ type, id, ...body }`. Replies: `{ type: 'result', id, value }`
  or `{ type: 'result', id, error }`. Cancel: `{ type: 'cancel', id }`.
- The host validates every message against a schema and drops unknown types.
- The host pings every second and removes a page that hasn't answered for 8
  seconds. It disconnects a page that sends more than 500 requests in a second.

What the SDK asks the host for:

| SDK API | Message | Host does |
|---------|---------|-----------|
| `useQuery`, `grafana.query`, `grafana.subscribe` | `query-subscribe` | Resolves a named binding from the bundle (or takes an inline request), runs it through the datasource plugin as the viewer, and streams `query-result`, `query-complete` or `query-error`; see [data.md](data.md#the-path-of-a-query) |
| `grafana.datasources.list` | `datasources` | Lists datasources the viewer can query |
| `GrafanaPanel` | `panels`, `panel-data` | Renders the plugin in its own isolated frame over the reserved slot |
| `useTimeRange`, `TimeRangePicker` | `range`, `timezone`, `refresh` | Updates the shared range and reruns queries |
| `useUrlState` | `url-state` | Stores the value as a `page-<key>` parameter on Grafana's URL |
| `grafana.userStorage` | `user-storage` | Stores the value for this viewer and page |
| `grafana.navigate`, `<a href>` | `navigate`, `open-link` | Navigates Grafana; asks before opening other sites |
| `grafana.downloadCsv`, `downloadFile` | `download-csv`, `download-file` | Asks the viewer, then saves |
| `grafana.actions.*` | `actions-list`, `action-execute` | Shows a review dialog; runs on approval |
| `grafana.mcp.*`, `useMcpTool` | `mcp-connection`, `mcp-connect`, `mcp-call` | Calls the viewer's MCP connections; reviews reads once per server and each write |
| `openAssistant`, `useInlineAssistant` | `assistant-open`, `assistant-generate` | Opens or runs Grafana Assistant |
| `usePageTool` | `tools-register`, `tool-result` | Exposes page tools to Assistant |
| `pageReady()` | `ready` | Marks startup complete; 15 seconds allowed |

The host sends `context` (time range, theme, timezone, viewer), `ping`,
`query-*` results, `panel-event` (hover and select on native panels) and
`capture` (snapshot clicks and scrolling).

Hosts that embed pages outside Grafana implement a subset of these messages. A
request a host lacks fails with "… is only available inside Grafana".

## Security model

- The host validates every request independently. A page cannot widen what the
  viewer may do.
- Queries run as the viewer, so a page shows each person only what their
  datasource permissions allow.
- Actions, MCP writes, downloads and external links need the viewer's explicit
  approval, and fresh user activation where it applies.
- Nothing that acts as the viewer runs while the page is hidden, is an
  unaccepted candidate, or is being captured.
- Source is readable by anyone who can open the page. Keep secrets and
  personal data out of `src/`, `assets/` and `public/`.

## Limits

| What | Limit |
|------|-------|
| Project | 1,024 files (the SDK uses about 177), 10 MiB of source |
| Built page | 16 MiB of script, styles and inlined assets |
| One gcx push write | A text file of up to 120 KiB |
| Queries and bindings | See [data.md](data.md#limits) |
| Native panels | 32 mounted, 4096×2400 pixels each |
| URL state | 2,000 URL-encoded characters |
| User storage | Values up to 64 KiB |
| Page tools | 12 |
| Startup | `pageReady()` within 15 seconds |
| Snapshot | Up to 2,400 pixels of page, finished within about 25 seconds |

## Symptoms and causes

| Symptom | Likely cause |
|---------|--------------|
| Blank page, "STARTUP_TIMEOUT" | `PageProvider` or `pageReady()` removed from `src/main.tsx`, or the first render throws |
| `fetch` fails with a CSP error | Network access outside Grafana; use a binding, or an MCP server for outside data |
| Fonts fall back to a system font | A remote font URL; import an `@fontsource` package |
| Form does nothing on Enter | Native submission is blocked; use `onClick` and `onKeyDown` |
| Menu or tooltip is cut off | Overlays can't leave the frame; place them inside it |
| "… is only available inside Grafana" | The page runs on a host without that bridge feature |
| Wrong or missing data | See [data.md](data.md#symptoms-and-causes) |
