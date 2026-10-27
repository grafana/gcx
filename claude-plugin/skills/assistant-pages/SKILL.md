---
name: assistant-pages
description: >-
  Edits Grafana Assistant Pages (the React apps the Assistant builds and
  publishes in the page catalog) from outside Grafana with gcx: find a page,
  pull its source project, change it locally, check it builds on the stack,
  and publish it as the page's next version. Use when asked to change, fix or
  restyle an existing Grafana Page, or to inspect its code or queries. Not for
  dashboards (use manage-dashboards) and not for creating a brand-new page,
  which still starts in the Assistant.
---

# Assistant Pages

A Grafana Page is a React/TypeScript project that the stack compiles in a
sandbox and renders in a sandboxed iframe. gcx moves the whole project between
the stack and a local directory, so you edit it with normal file tools and the
stack does the build. These commands are experimental and need Pages and Pages
import and export enabled on the stack.

## Workflow

1. Find the page. A page URL works anywhere a page id does.

   ```bash
   gcx assistant pages list --section mine
   gcx assistant pages list --section discover --search radar
   gcx assistant pages get PAGE_ID
   ```

2. Pull its source into a fresh directory.

   ```bash
   gcx assistant pages pull PAGE_ID --dir ./page
   ```

   `.gcx-page.json` in that directory records the page and the version you
   pulled. Keep it: push needs it.

3. Edit the files (see [Project layout](#project-layout)).

4. Check it compiles on the stack, without publishing:

   ```bash
   gcx assistant pages push --dir ./page --dry-run
   ```

   A compile error fails the command with the TypeScript output. Fix and repeat.

5. Publish it as the page's next version. Title, folder and sharing stay as
   they are; earlier versions stay in the page's history.

   ```bash
   gcx assistant pages push --dir ./page
   ```

   Pushing again from the same directory publishes the version after that.

Publishing changes a page other people may be looking at. Do it when the user
asked for the change to go live; otherwise stop at `--dry-run` and report.

## Project layout

- `src/main.tsx` mounts the app; most work happens in `src/App.tsx` and
  `src/components/`.
- `pages.bindings.json` holds the page's named queries: `title`,
  `datasourceUid` and `query` (PromQL `expr`, LogQL, TraceQL, with `instant` or
  `range`). Components read them with `useQuery('name', { variables })`;
  `${name}` in a query is filled from `variables`.
- `packages/pages-sdk/` is the `@grafana/pages` SDK. Read
  `packages/pages-sdk/src/index.ts` for what it exports (`useQuery`,
  `GrafanaPanel`, `TimeRangePicker`, `useTimeRange`, `useUrlState`,
  `usePageTool`, `useInlineAssistant`, `openAssistant`, `grafana.navigate`).
  Do not edit it.
- `package.json` and `pnpm-lock.yaml` are fixed by the stack's sandbox image.
  Only use the dependencies already listed; the build cannot install new ones.

## Rules the sandbox enforces

- No network from the page: data comes only through bindings, which run in
  Grafana as the viewer. Check a query with `gcx metrics query`, `gcx logs
  query` or `gcx traces query` before you bind it.
- A page holds at most 256 files and 16 MiB of source.
- Native panels (`GrafanaPanel`) render inside their own frames; keep overlays
  and fixed drawers away from them.

## Pitfalls seen on real pages

- `useQuery` keeps the previous result while a new one loads. When variables
  change (another service selected), track which variables the frames belong
  to, or the panel briefly shows the previous selection's data.
- A `logs` panel given zero frames shows "Unable to render panel data". Render
  an empty-state message instead of the panel when a result has no rows.
- The host runs one `useInlineAssistant` generation per page at a time. Queue
  generations, or a second one fails with "An inline generation is already
  running".
- Instant queries read only the newest sample, so a short scrape gap empties
  the page. Prefer `last_over_time(metric[$__range])` and
  `rate(metric[$__range])`.
- In-page URL state appears on the host URL as `page-<key>` parameters.

## Errors

| Message | Meaning | Do |
|---------|---------|-----|
| `was pulled at version N` | Someone published a newer version | Pull into a new directory, reapply your changes, push |
| `does not accept pushed page source yet` | The stack's Assistant predates source edits | Stop; the change cannot be published from gcx on this stack |
| `the page did not build` | Compile error, output included | Fix the file and line it names, run `--dry-run` again |
| HTTP 403 | You cannot edit the page, or import and export is off | Ask the page's owner, or check the stack |
