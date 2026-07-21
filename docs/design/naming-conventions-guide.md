# Command Naming Conventions

> How to name gcx commands - which verb to use and where a command sits in
> the tree. The prescriptive rules for resource kinds, files, config keys,
> and flags live in [naming.md](naming.md).

## Behavior determines the name

A command is named for what the *user* experiences: what it acts on, whether
you address one thing by ID, whether it returns one item or many, and whether
it changes anything. It is never named for the HTTP method, the API path, or
how it is wired internally.

A read-one is `get` whether the backend uses GET, POST, or three calls under
the hood.

## The CRUD verbs

| Verb | Meaning |
|------|---------|
| `list` | Enumerate zero or more independent things (`slo definitions list`) |
| `get` | Retrieve exactly one thing, addressed by ID (or a singleton) |
| `create` | Make a new thing - fails if it already exists |
| `update` | Change an existing thing - fails if it doesn't exist |
| `delete` | Remove an existing thing |

Two create-or-update verbs exist, distinguished by workflow, not transport:

- `upsert` - one invocation creates the thing if absent, updates it if
  present. Do not split a true upsert into fake `create`/`update` commands:
  that falsely promises existence checks and invites read-then-write races.
- `push` / `pull` - the manifest (GitOps) workflow: `push` applies local
  manifest files to the remote, `pull` writes remote resources to local files.
  This is a different workflow from `upsert`, not a synonym.

One documented exception: `gcx resources get` takes kubectl-style selectors
(`dashboards`, `dashboards/foo,bar`) and may return one item or many.

## View verbs

Use a view verb (`status`, `timeline`, `inspect`, `diff`, `stats`, `report`,
`describe`) only when the output *materially differs* from a plain read.
Example: `kg entities inspect` returns an RCA timeline and related entities -
a diagnostic analysis, not the entity's fields - so it earns `inspect`.

`show` is not canonical in gcx (it has meant "read one", "list many", and
"render a view" at different times). `summary` is not canonical either;
existing `summary` commands get reclassified per command as `stats`, `status`,
or `report` based on what they actually output.

## Domain verbs

When CRUD would lie, use a domain verb: you `close` an incident or
`acknowledge` an alert - you don't "update" it closed. Domain verbs like
`resolve`, `silence`, and `escalate` must each be registered in the
allowed-operation registry with a written definition of what they mean.

## Where does a list command live? (the addressability rule)

This is the most important rule: choosing between
`gcx <area> things list` and `gcx <area> list-things`.

**If a thing has its own ID** - you can fetch exactly one with it - it gets
its own noun group:

```
gcx k6 load-tests list
gcx k6 load-tests get <id>
```

**`list-things` is reserved for exactly two cases:**

1. Plain value catalogs with no ID of their own: `cloudwatch list-regions` -
   there is no `region get`; a region is just a value.
2. Sub-lists addressed by the *parent's* ID: `alert-groups list-alerts
   <group-id>` - the ID you pass belongs to the group, not to an alert.

"It only has a list command today" is explicitly *not* a reason to use the
compound. The day the subject grows a `get`, you'd face a breaking rename.
The test is "does it have its own ID?", never "how many verbs does it have?".

The heuristic: **the type of ID you pass commands the nesting**. A worked
example with two kinds of ID in one command - `collections add-conversations
<collection-id> <saved-id>...`: the first positional is
the collection's ID, so the command nests under `collections`; the
saved-conversation IDs are payload being added, not the addressed subject.

Nested noun groups for sub-resources (`alert-groups alerts list`) are
rejected - use the parent-ID compound instead.

## Pre-GA renames are clean breaks

Before v1.0.0, renames ship with no aliases and no compatibility forwarders -
the migration guide documents every old → new mapping instead. Every existing
command resolves to exactly one of: **keep**, **ratify** (intentional
exception), **rename**, or **remove**.
