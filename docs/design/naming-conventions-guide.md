# How to name your commands

This is a guide on how to name your gcx commands: Which verb to use and where a command sits in the tree.


## CRUD verbs

Unless they absolutely do not work for your use case, use these verbs for CRUD type operations:

| Verb | Meaning |
|------|---------|
| `list` | Enumerate zero or more independent things (`slo definitions list`) |
| `get` | Retrieve exactly one thing, addressed by an ID of the type you're getting |
| `create` | Make a new thing - fails if it already exists |
| `update` | Change an existing thing - fails if it doesn't exist |
| `delete` | Remove an existing thing |

There are two create-or-update verbs, depending on workflow:

- `upsert` - one invocation creates the thing if absent, updates it if
  present. Do not split it into fake `create`/`update` commands:
  that hides behaviour.
- `push` / `pull` - the manifest (GitOps) workflow: `push` applies local
  manifest files to the remote, `pull` writes remote resources to local files.
  This is a different workflow from `upsert`.

There is one documented exception to these rules: `gcx resources get` takes kubectl-style selectors
(`dashboards`, `dashboards/foo,bar`) and may return one item or many.

## View verbs

Default to using `get`. Use a view verb (`status`, `timeline`, `inspect`, `diff`, `stats`, `report`,
`describe`) only when the output differs from a straight read of the resource. For example: `kg entities inspect` returns an RCA timeline and related entities, which is a diagnostic analysis, not the entity's fields.

## Domain verbs

Only diverge from the CRUD verbs when they are not accurate for your domain. For example, you `close` an incident or
`acknowledge` an alert - you don't "update" it closed. Domain verbs like
`resolve`, `silence`, and `escalate` must each be registered in the
allowed-operation registry with a written definition of what they mean.

## Where does a list command live?

This is the most important rule: choosing between
`gcx <area> things list` and `gcx <area> list-things`.

If a thing has its own ID - meaning you can fetch exactly one resource with it - it gets
its own noun group:

```
gcx k6 load-tests list
gcx k6 load-tests get <id>
```

**`list-things` is reserved for two cases:**

1. When a resource is listable but they have no ID of their own: `cloudwatch list-regions` -
   there is no `region get`; a region is just a value.
2. When you filter a list based on the *parent's* ID: `alert-groups list-alerts
   <group-id>` - the ID you pass belongs to the group, not to an alert.

