# How to name your commands

This is a guide on how to name your gcx commands: Which verb to use and where a command sits in the tree.


## CRUD verbs

Unless they absolutely do not work for your use case, use these verbs for CRUD type operations:

| Verb | Meaning |
|------|---------|
| `list` | Enumerate zero or more independent things (`slo definitions list`) |
| `get` | Retrieve exactly one thing - addressed by an ID of the type you're getting, or a singleton with no ID (`appo11y settings get`) |
| `create` | Make a new thing - fails if it already exists |
| `update` | Change an existing thing - fails if it doesn't exist |
| `delete` | Remove an existing thing |

There are two create-or-update verbs. The workflow decides which one - not how many
things one invocation touches:

- `upsert` - the direct create-or-update workflow: the caller supplies one or more
  explicit subjects and never chooses create vs update. Multi-subject input is
  processed independently, in order, and may be non-atomic. Do not split a true
  upsert into fake `create`/`update` commands: that hides behaviour.
- `push` / `pull` - the manifest (GitOps) workflow: `push` applies local manifest
  files to the remote with validation, diff, and a summary, and does not delete
  remote resources omitted from the input. `pull` writes remote resources to
  local files.

There is one documented exception to these rules: `gcx resources get` takes kubectl-style selectors
(`dashboards`, `dashboards/foo,bar`) and may return one item or many.

## View verbs

Default to using `get`. Use a view verb (`status`, `timeline`, `inspect`, `diff`, `stats`, `report`,
`describe`) only when the output differs from a straight read of the resource. For example: `kg entities inspect` returns an RCA timeline and related entities, which is a diagnostic analysis, not the entity's fields.

Approved exceptions to these rules are recorded case by case - for example
`datasources health` is an owner-approved keep, not a pattern to copy.

## Domain verbs

Only diverge from the CRUD verbs when they are not accurate for your domain. For example, you `close` an incident or
`acknowledge` an alert - you don't "update" it closed. Domain verbs like
`resolve`, `silence`, and `escalate` must each be registered in the
allowed-operation registry with a written definition of what they mean.

## Where does a list command live?

This is the most important rule: choosing between
`gcx <area> things list` and `gcx <area> list-things`.

Use `things list` for an independently addressable resource group - the thing has its
own identity:

```
gcx k6 load-tests list
gcx k6 load-tests get <id>
```

A catalog child with no parent identity may also keep this shape: `incidents severities list` -
severity rows have IDs, and the list takes no parent ID.

**`list-things` is reserved for two cases:**

1. A discovery or catalog facet that is not independently addressable:
   `cloudwatch list-regions` - a region is just a value.
2. A parent-scoped collection that is not independently addressable, where the required
   positional identity belongs to the parent: `alert-groups list-alerts <group-id>` -
   the ID you pass belongs to the group, not to an alert.

The number of commands implemented for the subject today is not part of the rule.

The same placement rule applies to operations other than `list`: when the required
positional belongs to the parent and the child is not independently addressable, the
shape is `<parent> <operation>-<child> <parent-id>` - for example
`k6 load-tests delete-schedule <load-test-id>` or
`collections add-conversations <collection-id> <id>...`.

