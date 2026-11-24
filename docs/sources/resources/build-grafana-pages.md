---
title: Build Grafana Pages
description: Use gcx to write a Grafana Page as a local React project, build it on your Grafana Cloud stack, check how it renders, and publish it, with your editor or a coding agent.
labels:
  products:
    - cloud
  stage: experimental
keywords:
  - gcx
  - assistant
  - pages
weight: 110
---

# Build Grafana Pages

A Grafana Page is a live web page on your Grafana data, such as a report, an incident summary, or a slideshow. It's a small React and TypeScript project that your stack builds and serves in Grafana, where its queries run as each viewer. Use `gcx` to write a page in a local directory, with your own editor or a coding agent, and let the stack build it. **Pages commands are experimental and require Grafana Cloud.**

## Before you begin

- [Configure a gcx context](../../configuration/) for a Grafana Cloud stack with Grafana Pages enabled and a version of Grafana Assistant that accepts gcx's Pages commands.
- To publish or pull a published page, sign in with `gcx login`. A service account token can build new drafts, but can't publish or edit published pages.

Commands use your selected context. Add `--context CONTEXT` to select another context. These commands don't run Grafana Assistant.

## Start a page

Create a draft page and write its project into a new directory:

```shell
gcx assistant pages create --title "Checkout health" --dir ./checkout-health
```

The directory holds the whole project: `page.json`, `pages.bindings.json` for the page's queries, `src/App.tsx`, and the Pages SDK with its documentation under `packages/pages-sdk/`. Start with `packages/pages-sdk/README.md`. The `.gcx-page.json` file links the directory to the draft; keep it.

The draft is private to you until you publish it.

## Build and check the page

To build the page and check how it renders, follow these steps:

1. Add a named query to `pages.bindings.json`. Replace `DATASOURCE_UID` with the UID of a Prometheus data source:

   ```json
   {
     "requests": {
       "title": "Checkout requests per second",
       "datasourceUid": "DATASOURCE_UID",
       "query": { "expr": "sum(rate(http_requests_total{service=\"checkout\"}[5m]))", "range": true }
     }
   }
   ```

1. Show it in `src/App.tsx`:

   ```tsx
   import { GrafanaPanel, TimeRangePicker, useQuery } from '@grafana/pages';

   export function App() {
     const requests = useQuery('requests');
     return (
       <main>
         <h1>Checkout health</h1>
         <TimeRangePicker />
         <div data-page-sources='["requests"]'>
           <GrafanaPanel data={requests} pluginId="timeseries" title="Requests per second" />
         </div>
       </main>
     );
   }
   ```

1. Build it on your stack:

   ```shell
   gcx assistant pages push --dir ./checkout-health
   ```

   Push sends the files that changed. If the page doesn't build, the command lists the type and build errors, and the draft keeps its previous version.

1. Render the latest build, with live data, as an image:

   ```shell
   gcx assistant pages snapshot --dir ./checkout-health --output-dir ./snapshots
   ```

1. Open the image and check the layout and data. A page that builds can still render incorrectly.

The output of `push` includes the draft's address in Grafana.

## Change a published page

Copy a published page into a new draft:

```shell
gcx assistant pages list --section mine
gcx assistant pages pull PAGE --dir ./checkout-health-copy
```

Replace `PAGE` with the page's ID or link. The directory must be new or empty. Edit, push, and check the page as before. Publishing it makes your changes the page's next version.

A copy leaves out the page's agent guidance and the SDK's Markdown docs, which another person may have written, and lists them under `omitted` in JSON or YAML output. The stack builds the copy with the template's guidance.

If the draft changes in Grafana, for example when you ask Grafana Assistant to change it, download it again before you edit:

```shell
gcx assistant pages pull --dir ./checkout-health-copy
```

This refuses while the directory has changes you haven't pushed. Push them first, or add `--force` to discard them.

## Publish the page

Publishing shares the page:

```shell
gcx assistant pages publish --dir ./checkout-health --folder FOLDER_UID --category report
```

Everyone who can view the folder can open the page. To publish without a folder, use `--unlisted`: only you can open the page, unless you add `--link-sharing` to let anyone in your organization with the link open it.

Publishing uses the draft's last successful build, not your local files. If the directory has changes you haven't pushed, `publish` warns you and publishes the earlier build, so push first.

A page you pulled, or published before, keeps its folder and sharing, and earlier versions stay in its history. To change its folder or sharing, use Grafana, then publish without `--folder`, `--unlisted`, or `--link-sharing`.

## Use Pages with coding agents

The `grafana-pages` agent skill explains the page project, the SDK, query bindings, and how pages run, and asks agents to build a Page instead of an artifact or a standalone HTML file when your stack has Pages. To install it, refer to [Use gcx with agents](../../use-gcx-with-agents/).

## Limits

- `push` writes text files of up to 120 KiB. Use SVG for images you add.
- Pages build with the packages in the project's `package.json`; gcx can't install others.
- A page runs in an isolated frame without network access. Its data comes from its queries, which run as each viewer.
