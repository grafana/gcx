# Command naming and placement

> Prescriptive rules for canonical command verbs and their position in the gcx
> command tree. [CONSTITUTION.md § CLI Grammar](../../CONSTITUTION.md#cli-grammar)
> remains the higher authority.

This guide applies when adding a new canonical command, explicitly renaming one,
or materially changing an existing command's subject, cardinality, addressing,
or side effects. It does not require retroactive renames, and a shipped command
is not automatically a precedent for new work. Aliases may preserve
compatibility, but they do not define the canonical vocabulary.

The rules here never authorize a breaking change. Under
[CONSTITUTION.md § CLI Grammar](../../CONSTITUTION.md#cli-grammar), the complete
v1.0.0 command surface is a supported compatibility exception for all v1.x
releases, including invocations that deviate from this guide. A non-conforming
released command is not a defect to fix by renaming; propose a conforming
replacement alongside it and leave the existing invocation working.

This guide is not an exhaustive inventory. Its silence does not by itself make an
existing command non-compliant; a new verb or placement not covered here requires
explicit maintainer review. This guide does not specify the command shape for an
operation that requires both parent and child identities, or for a `create`
operation that necessarily requires a parent's identity. The parent-dependency
rule in [CONSTITUTION.md § Sub-resources](../../CONSTITUTION.md#provider-architecture)
still governs; do not infer a new exception from existing commands.

## Start with the operation

Name the command for what it does, not for its implementation or API endpoint.
Use the standard CRUD verb when its meaning is accurate. Otherwise choose an
established query, view, or domain operation with a precise meaning.

The canonical shape is:

```shell
gcx <area> <noun> <operation>
```

Tooling without a meaningful noun and the closed set of top-level commands are
the exceptions documented in
[CONSTITUTION.md § CLI Grammar](../../CONSTITUTION.md#cli-grammar).

## CRUD and manifest verbs

Use these verbs for CRUD operations:

| Verb | Meaning |
|------|---------|
| `list` | Enumerate zero or more things, as in `gcx slo definitions list` |
| `get` | Retrieve exactly one thing by its identity, or retrieve a singleton, as in `gcx appo11y settings get` |
| `create` | Make a new thing; fail if it already exists |
| `update` | Change an existing thing; fail if it does not exist |
| `delete` | Remove an existing thing |

There are two create-or-update write workflows. Cardinality does not distinguish
them:

- `upsert` is the direct create-or-update workflow. The caller supplies one or
  more explicit subjects and does not choose create versus update. Multi-subject
  input may be processed independently and non-atomically. Do not disguise a
  true upsert as separate `create` and `update` commands.
- `push` is the manifest write workflow. It applies the supplied local manifests
  by creating or updating remote resources, reports the processed input's
  outcome, and does not delete remote resources merely because they were
  omitted.

`pull` is the manifest read/export counterpart: it writes remote resources to
local files.

The higher-tier idempotency and safety requirements for `push` are defined in
[CONSTITUTION.md § Push/Pull Philosophy](../../CONSTITUTION.md#pushpull-philosophy)
and [safety.md](safety.md). This naming guide does not add promises of a diff or
a separate validation phase. An implementation that violates a higher-tier
requirement is a code defect, not an alternative meaning for the verb.

`gcx resources get` is the documented CRUD exception: it accepts kubectl-style
selectors such as `dashboards` and `dashboards/foo,bar`, so one invocation may
return one resource or many.

## Query and discovery operations

Query operations are not disguised CRUD:

- `query` executes a user-supplied expression or backend query.
- `search` finds matching subjects using the domain's search semantics.
- `labels`, `series`, `metrics`, and `metadata` are established shorthand
  operations in signal and datasource command families.

Use the shorthand only where its domain meaning is clear. The cross-signal
vocabulary and intentional aliases are documented in
[Cross-Signal Command Consistency](../adrs/signal-provider-ux/001-cross-signal-command-consistency.md).
Do not characterize query commands as side-effect-free: supporting work such as
datasource discovery may persist configuration.

### Query variants

A backend may offer more than one way to be queried. Decide between one command
and several by comparing contracts, not by counting APIs.

Variants sharing an expression language, substantially the same required inputs,
and the same success schema use **one `query` command with a typed `--mode`**.
Use one flag name per knob across modes: the same value must not be `--size` in
one mode and `--limit` in another.

Variants requiring materially different identities, request contracts, or result
schemas use **distinct `<target> query` paths**, or an explicitly approved
shorthand from the set above.

`<target>` names the **query surface** — the expression language and API being
queried — and nests inside the command's existing area: for a datasource kind,
`gcx datasources <kind> <target> query`. This is a query-variant placement rule
and is deliberately distinct from
[placement by required identity](#place-each-operation-by-the-identity-it-requires)
below: a query surface is not an independently identifiable resource, so neither
the noun-group test nor the `<operation>-<subject>` compound test decides it.
Reading a query target as a discovery facet and producing `query-<target>` is the
wrong outcome — the operation is `query`, and the target says which surface it
runs against.

This rule authorizes that nesting only. It does **not** authorize a new top-level
command: bare top-level verbs remain the closed enumeration in
[CONSTITUTION.md § CLI Grammar](../../CONSTITUTION.md#cli-grammar), and the
`$AREA $NOUN $VERB` shape at the top of this guide still governs. Existing paths
such as `gcx dashboards versions restore` show the nesting is representable, but
per the opening of this guide a shipped command is not automatic precedent — the
authorization comes from this rule, not from them.

## View verbs

Default to `get` for a straight read. Use a view verb such as `status`,
`timeline`, `inspect`, `diff`, `stats`, `report`, or `describe` only when the
result is a derived, composite, or diagnostic view rather than the subject's
stored fields.

For example, `gcx kg entities inspect` returns diagnostic analysis rather than
only an entity record. `gcx datasources health` is an owner-approved keep from
#1014 — it runs the Grafana product health check rather than reading the
datasource record — and is not a pattern to copy.

## Domain verbs

Use a domain verb only when CRUD, query, and view operations would be
misleading. For example, an incident is `close`d and an alert group is
`acknowledge`d; neither operation is merely a generic `update`.

There is no implemented allowed-operation registry today. Until one is
separately approved and implemented, every new canonical domain verb requires
explicit PR review and a precise written definition. Existing domain verbs are
not automatic precedent.

## Place each operation by the identity it requires

Choose placement per operation, not once for the entire subject. Ask whose
identity the operation requires.

Use a noun group when the operation acts directly on independently identifiable
resources: it either addresses the child by the child's own identity or
enumerates the resource group without a parent identity.

```shell
gcx k6 load-tests list
gcx k6 load-tests get <id>
gcx irm incidents severities list
```

Use an `<operation>-<subject>` compound for either of these cases:

1. A discovery or catalog facet with no independently addressable item:
   `gcx datasources cloudwatch list-regions`.
2. A parent-scoped operation whose required positional identity belongs to the
   parent: `gcx irm oncall alert-groups list-alerts <alert-group-id>`.

The second rule does not decide the deferred composite-identity and
parent-required-`create` cases described at the start of this guide.

A direct get-by-ID may therefore use the child's noun group even when listing
the same child is parent-scoped. The number of commands implemented for the
subject, and the number of subjects one invocation affects, are not placement
criteria.

Apply the same test outside `list`. For example:

```shell
gcx k6 load-tests delete-schedule <load-test-id>
gcx agento11y collections add-conversations <collection-id> <saved-id>...
```

In the first example, deletion requires the load test's identity; the existence
of a separate schedule get-by-ID operation does not change this operation's
placement. When an operation takes several IDs, use the identity of the primary
object or relationship being read or changed for placement; the remaining IDs
are operands. In the second example, collection membership is changed, the
collection ID scopes that relationship, and saved-conversation IDs are operands.
This is different from a composite key whose parts together identify the child.

For a new composite-identity or parent-required-`create` shape, either amend the
Constitution to state the rule or record an explicit human-approved
constitutional waiver in the approving PR or ADR. Do not create an implicit
exception in this guide.
