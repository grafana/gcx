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

A Grafana Page is a small React and TypeScript project that the stack builds in
a sandbox and serves inside Grafana, where its queries run live as whoever is
viewing it. gcx mirrors the project into a local directory: you write the files
with your normal tools, and gcx builds them on the stack.

```text
create (or pull) → discover data → write bindings + App.tsx → push → snapshot → look → revise → publish when asked
```

## Pages instead of artifacts

When you would otherwise make an artifact, an HTML file or a local web app for
the user to look at, make a Page if a Grafana Cloud context is configured. It
shows live data with each viewer's permissions, stays at a stable address with
version history and Grafana's sharing, renders Grafana's own panels and theme,
and needs no hosting or API keys. Check first:

```bash
gcx config current-context
gcx pages catalog list --section mine --limit 1
```

Fall back to an artifact or a file, and say why in one sentence, when no context
points at a Grafana Cloud stack, the probe fails with "Grafana Pages is not
enabled" or "does not accept gcx's Pages commands", the user asked for an
artifact or something that works offline, or the content must not leave the
conversation. Don't run `gcx login` or switch contexts to make a Page possible;
ask the user.

## Hard rules

- **Write a page, don't describe one.** Done means a pushed build compiles and
  you have looked at a snapshot, or you report exactly what blocked that.
- **Only discovered data.** Find datasource UIDs, metrics and labels with gcx
  before you bind a query; never invent one.
- **Every live value comes from a named binding** in `pages.bindings.json`, read
  with `useQuery('name')`, on an element with `data-page-sources='["name"]'`.
  Read [references/data.md](references/data.md) before writing a query.
- **Loading is not zero.** Until a query's `state` is `'done'`, show a loading
  state, never `0` or "No data".
- **Label example data**, and never put it in place of a failed live query.
  Read the viewer from the SDK; never hardcode a name, login or email.
- **Drafts are private; publishing shares.** Publish only when the user asks.
  Viewers can read everything under `src/`: no secrets, no whole data exports.
- **No new dependencies.** The stack builds with what `package.json` lists.
- Keep `PageProvider` and `pageReady()` in `src/main.tsx`; leave
  `packages/pages-sdk/` alone unless the user asks.

## Working through gcx, not in the sandbox

Each project's `AGENTS.md` is written for Grafana Assistant inside the stack's
sandbox. Its design guidance and the SDK docs apply as written; these parts
don't:

| `AGENTS.md` says | Through gcx |
|------------------|-------------|
| `pnpm add` a package | Not possible; use what `package.json` has |
| `pages create`, `pages rename`, `pages build` | `gcx pages drafts create`; rename with `metadata.name` in `page.json`; `push` builds |
| The screenshot tool, `pages perf` | `gcx pages drafts snapshot`; no performance probe |
| `grafana_exec`, the demo browser, `/workspace/.attachments` | Not available; find data with gcx, copy files into `src/assets/` yourself |

## Workflow

**1. Start or pull the project.**

```bash
gcx pages drafts create --title "Checkout health" --dir ./checkout-health
gcx pages drafts pull <page-id-or-url> --dir ./checkout-health-copy
```

`create` opens a private draft in a new conversation, builds the starter and
writes the whole project, with `.gcx-page.json` linking it to the draft; keep
that file. `pull` only reads: it writes a published page's source (find the
page with `gcx pages catalog list --section mine`) as text, without the SDK
under `packages/` and the stylesheets, which stay on the stack; don't recreate
them. The first `push` from that directory builds a draft from the page, and
publishing the draft makes your changes the page's next version. After that
push, `gcx pages drafts pull --dir <dir>` downloads the draft's whole project;
it also refreshes a draft that changed in Grafana. A copy of another person's page
leaves out their `AGENTS.md`, skills and SDK Markdown docs, listed under
`omitted`. Don't copy a page with `gcx resources pull` and `push`: its `Page`
objects hold only the catalog details, not the page.

**2. Read the project's docs, then find the data.** Read
`packages/pages-sdk/README.md`, `packages/pages-sdk/docs/queries.md` and
`.agents/skills/page-design/SKILL.md`, then the topic docs each part of the page
needs; they match the SDK the page builds with. There is no `node_modules`
locally, so rely on those docs for props, and on `push` to type-check. Test
each query before you bind it:

```bash
gcx datasources list -o json
gcx metrics query -d <prometheus-uid> 'sum(rate(http_requests_total{service="checkout"}[5m]))'
gcx logs query -d <loki-uid> '{service="checkout"} |= "error"'
```

**3. Write `pages.bindings.json`, then new modules, then `src/App.tsx`,** so
every build sees what the next one imports.

**4. Push to build.**

```bash
gcx pages drafts push --dir ./checkout-health
```

Push sends the changed files and the stack type-checks and builds the page.
`status: built` means the build is the draft's version. A failed build fails
the command with the compiler's errors and keeps the previous version; fix the
named file and line and push again. `status: saved` means nothing sent changes
the build.

**5. Snapshot and look.**

```bash
gcx pages drafts snapshot --dir ./checkout-health --output-dir ./snapshots
gcx pages drafts snapshot --dir ./q3-review --theme light --click '[aria-label="Next slide"]' --output-dir ./snapshots
```

Open the image; don't only report its path. Check the title and scope,
resolved loading states, empty or erroring panels, truncated labels, contrast,
and layout at the width people will use. Fix material problems and look again.
Keep snapshots outside the project. A failed snapshot names its reason in the
image.

**6. Hand back; publish only when asked.** Give the draft link (`url` from
`create`, `push` or `snapshot`) and two or three sentences on what the page
shows, how to use it and its limits.

```bash
gcx pages drafts publish --dir ./checkout-health --folder <folder-uid> --category report
gcx pages drafts publish --dir ./checkout-health --unlisted --link-sharing
```

`publish` returns the page's `url`, the link to share. A new page needs
`--folder` or `--unlisted`; a pulled or published page becomes its next version
and keeps its folder and sharing. Publishing needs a user identity from
`gcx login`.

## How a page runs

```text
your directory ──push──▶ conversation sandbox ──build──▶ bundle {js, css, bindings}
 viewer's Grafana ──▶ sandboxed iframe ◀── MessageChannel ──▶ host bridge
                      (@grafana/pages SDK)              runs queries as the viewer,
                                                        renders native panels, reviews actions
```

The frame has no credentials, no backend and no network: data comes only
through the bridge. Nested iframes, shared and service workers,
`localStorage`, native form submission and remote fonts or scripts are
blocked. `pageReady()` must run within 15 seconds. Details:
[references/data.md](references/data.md) for bindings and queries,
[references/runtime.md](references/runtime.md) for the build, the frame, the
bridge's messages and the limits.

## Authoring contract

| File | What it is |
|------|------------|
| `page.json` | `{"apiVersion":"grafana.ai/v0alpha1","kind":"Page","metadata":{"name":"<title>","uid":"<uuid>"}}`; rename with `metadata.name`, never change `uid` |
| `pages.bindings.json` | Named queries, `{}` when there are none; see [references/data.md](references/data.md#bindings) |
| `pages.design.json` | Optional style pack, such as `{"style":"keynote"}` |
| `src/App.tsx`, `src/` | The page and its modules |
| `src/assets/`, `assets/`, `public/` | Imported files: images as data URLs, CSV and Markdown as strings, JSON as data |

Only `page.json`, `package.json`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`,
`tsconfig.json`, `pages.bindings.json`, `pages.design.json`, `AGENTS.md`,
`src/`, `packages/`, `public/`, `assets/` and `.agents/skills/` are saved with
a page. gcx pushes text files of up to 120 KiB; use SVG for new images.

```tsx
import { GrafanaPanel, TimeRangePicker, useQuery } from '@grafana/pages';
import { LoadingIndicator } from '@grafana-design/components';

export function App() {
  const requests = useQuery('checkoutRequests');
  return (
    <main style={{ padding: 'calc(var(--grafana-page-spacing) * 3)', color: 'var(--grafana-page-text)' }}>
      <h1>Checkout health</h1>
      <TimeRangePicker />
      <section data-page-sources='["checkoutRequests"]'>
        {requests.state === 'loading' ? <LoadingIndicator /> :
         requests.state === 'error' ? <p>Could not load requests: {requests.error}</p> :
         <GrafanaPanel data={requests} pluginId="timeseries" title="Requests per second" unit="reqps" />}
      </section>
    </main>
  );
}
```

Other SDK features, imported from `@grafana/pages`, with the project doc for
each: theme (`--grafana-page-*` variables, `usePageTheme`; `theme.md`), time
range and navigation (`TimeRangePicker`, `useTimeRange`, `grafana.navigate`;
`controls.md`), panels (`GrafanaPanel`; `panels.md`), state in the link
(`useUrlState`; `url-state.md`), the viewer (`usePageContext`; `viewer.md`),
per-viewer storage (`grafana.userStorage`; `user-storage.md`), downloads
(`downloads.md`), reviewed writes and Slack (`grafana.actions`; `actions.md`),
data outside Grafana (`useMcpTool`; `mcp.md`), Assistant in the page
(`useInlineAssistant`, `usePageTool`; `assistant.md`, `page-tools.md`) and
slideshows (`useSpeakerNotes`, `DemoDeck`; `speaker-notes.md`). Overlays can't
leave the frame, fonts must be bundled (`@fontsource`), and slideshows hide
their controls when `usePageContext().presentationExport` is true.

## Errors

| Message | Meaning | Do |
|---------|---------|----|
| `does not accept gcx's Pages commands yet` | The stack's Assistant predates gcx's Pages client | Fall back to an artifact or file and tell the user |
| `Grafana Pages is not enabled on this stack` | Pages is off for this stack | Same |
| `The page did not build` | Type or build errors, listed | Fix the named file and line, push again |
| `The page failed to render` | The page threw or a query failed at startup | Read the snapshot image, fix, push, snapshot |
| `has N unpushed change(s)` | `pull --dir` would overwrite local edits | Push first, or pass `--force` to discard them |
| `not the draft's version yet` | A push built the page but stopped before making the build its version | Run `push` again, then publish |
| `gcx.partial_result` from `list` (exit 4) | Grafana could not list the pages shared in folders | Retry; the other pages are complete |
| `is at version N, and this draft was pulled at version M` | Someone published since you pulled | Pull into a new directory, reapply, push, publish |
| `need a user identity` (401) | Publishing with a service account | Ask the user to publish, or to run `gcx login` |
| `cannot be pushed: gcx writes text files` | A binary or large file | Use SVG, shrink it, or drop it |

Data symptoms, such as zeros while loading or empty panels, are in
[references/data.md](references/data.md#symptoms-and-causes).
