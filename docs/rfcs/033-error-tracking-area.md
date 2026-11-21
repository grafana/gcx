# RFC 033: Error Tracking area

**Status**: proposed

**Issue**: [grafana/gcx#1846](https://github.com/grafana/gcx/issues/1846)

## Context

Grafana Error Tracking has no gcx commands. People and agents reach it through `gcx api`, and build the paths, the paging and the time ranges by hand. This RFC adds an experimental `gcx error-tracking` area, with the alias `et`, for projects, errors and their events, comments and assignees, releases, heatmaps and source maps.

### Scope

**In scope:** the commands below, all experimental.

**Out of scope:**

- `resources` pipeline support.
- Deleting source maps.

## Decision

### 1. CLI UX

```
gcx error-tracking  (alias: gcx et)
├── projects      list · get · use · create · update · delete · timeline
│   └── ingest-keys   rotate · delete-previous
├── errors        (alias: error)  list · get · inspect · update · timeline
│   ├── events        list · get
│   ├── fields        list
│   ├── activity      list
│   ├── comments      create · update · delete
│   └── assignees     list · add · delete
├── raw-events    list · get
├── releases      list · status
├── environments  list
├── transactions  list
├── fields        list · list-values <key>
├── users         list
├── heatmaps      list · get · create · update · delete · report · breakdown
└── sourcemaps    list · upsert
```

A post-deploy check and a triage:

```shell
gcx et releases status v1.2.3 --project checkout-api
gcx et errors list --project checkout-api --status new --since-release v1.2.3
gcx et projects use checkout-api
gcx et errors list --sort event-count --limit 10
gcx et errors inspect 42
gcx et errors update 42 --status resolved
```

`releases status` shows its new, regressed and last-seen sections as one query each, so an error can appear in more than one section.

`--project` takes a project ID, its name (case-insensitive) or name-ID, and `projects use` saves a default project for the stack. The flag wins over `GRAFANA_PROVIDER_ERRORTRACKING_PROJECT`, which wins over the saved default. Positional `<project>` arguments, as in `projects delete <project>`, never take the default.

Ten leaves use a verb other than a plain CRUD one. Five are view verbs that `docs/design/command-naming.md` already allows for derived views: `releases status`, `errors inspect`, `projects timeline`, `errors timeline` and `heatmaps report`. They only need an entry in the command-tree test data. One, `projects ingest-keys delete-previous`, is a canonical `delete-` compound. The other four are new verbs, and D4 asks maintainers to approve them:

| Leaf | What it does |
|---|---|
| `errors assignees add` | Adds a user to an error's assignees |
| `projects ingest-keys rotate` | Replaces a project's ingest key, with a grace period |
| `projects use` | Saves, shows or clears the default project |
| `heatmaps breakdown` | One heatmap cell split by project |

### 2. Resource adapters

None. This is a commands-only provider. The JSON for projects and heatmaps uses the Kubernetes envelope, with the kinds `Project` and `Heatmap`.

### 3. Auth & config

Error Tracking is a Grafana Cloud product, so the area is listed as Cloud-only and is unavailable on a context that is not Grafana Cloud. Commands use the context's Grafana credentials. The provider has one config key, `project`, the saved default project. `sourcemaps upsert` uploads with the context's Grafana Cloud token, which needs the `sourcemaps:write` scope and also `stacks:read`, because the command looks the stack up in the Grafana Cloud API. The upload is authorized for the stack ID the context already knows, and for the ID from that lookup only when the context knows none. The token goes only to an upload host that shares the Grafana server's registrable domain, or, for a Grafana Cloud stack whose server is on a private public suffix, a host one label beside it, when the lookup confirms both the stack ID and the server's host. A context whose Grafana server is on a custom domain normally shares neither with the upload host, so it cannot upload; it needs a context that points at the stack's own address.

### 4. Architecture

The provider lives in `internal/providers/errortracking/` and registers itself through a blank import. Outside that package, gcx gains the import, an entry that lists the area as Cloud-only, a CODEOWNERS line, the new commands in the command-tree test data, a paragraph in `ARCHITECTURE.md`, and a mention in `docs/sources/permissions.md` and `docs/sources/guides/available-resources.md`.

## Decisions for maintainers

Each decision needs written maintainer approval on this RFC, and each stands on its own.

**D1. New top-level area and alias.** Add `gcx error-tracking` with the alias `et`. If declined, the area moves to `gcx frontend error-tracking` with the same groups and leaves.

**D2. `--project` as a scope.** This is a waiver of CONSTITUTION § Provider Architecture (Sub-resources nest under their parent command), which says a resource that cannot be listed or addressed without a parent ID is exposed as `$PARENT $VERB-$CHILD $PARENT_ID`. In this area only, the project is a `--project` scope with a default, and the subject stays positional, instead of nesting errors under `projects` as that rule asks. `environments list`, `transactions list` and `fields list` are project facets: their values have no IDs of their own, so they stay plain nouns under the area and take the same `--project` scope. If declined, the area uses sub-resource compounds such as `projects list-errors <project>`, and the three facets become `list-environments`, `list-transactions` and `list-fields`.

**D3. Sub-resources as nested groups.** This is a waiver of CONSTITUTION § Provider Architecture (Sub-resources nest under their parent command), and of the placement and composite-identity rules in `docs/design/command-naming.md`. An error's events, fields, activity, comments and assignees, and a project's ingest keys, are noun groups under their parent, and each leaf takes the parent's ID first. The rules ask for a compound such as `list-events <error>` when the parent's identity is required. `errors events get <error> <event>`, `errors comments update` and `delete <error> <comment>`, and `errors assignees add` and `delete <error> <user>` take two identities, and `errors comments create <error>` and `sourcemaps upsert --project` are creates that need their parent. The guide says to record a waiver for these shapes in the approving RFC, and not to infer one from existing commands. If declined, the area uses compounds such as `errors list-events <error>`.

**D4. Four new verbs.** `add`, `rotate`, `use` and `breakdown`, as in the table above. Each verb can be approved or declined on its own; a declined verb's leaf is dropped or renamed.

## Consequences

- People and agents can triage Error Tracking from the terminal or CI without building API paths.
- One `projects use` lets most commands run without `--project`.
- The area is experimental and can change before Error Tracking's API is stable.
- `gcx resources` does not cover Error Tracking.
