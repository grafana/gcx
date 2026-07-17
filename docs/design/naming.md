# Resource and API Naming

> Naming conventions for resource kinds, file names, config keys, environment variables, flags, URL path patterns, and command operations.

---

## 9. Resource and API Naming

### 9.1 Resource Kind Names

Follow Kubernetes conventions: PascalCase singular.

```
Dashboard, Folder, AlertRule, ContactPoint
```

Plural form is used in selectors: `dashboards/my-dash`, `folders/`.

### 9.2 File Naming

Pull operations write files as `{Kind}.{Version}.{Group}/{Name}.{ext}`, grouped by
`GroupResourcesByKind`. Extension matches the source format (`.yaml`, `.json`).

Example: `Dashboard.v1alpha1.dashboard.grafana.app/my-dash.yaml`

The versioned directory name makes the API group and version unambiguous, which
is important when pulling multiple versions of the same resource type.

### 9.3 Config Key Naming

| Location | Convention | Example |
|----------|-----------|---------|
| YAML | kebab-case | `org-id`, `stack-id`, `api-token` |
| Env vars | SCREAMING_SNAKE | `GRAFANA_ORG_ID`, `GRAFANA_STACK_ID` |
| Provider env | `GRAFANA_PROVIDER_{NAME}_{KEY}` | `GRAFANA_PROVIDER_SLO_TOKEN` |

Env var keys are normalized: underscores → dashes for provider key matching.

### 9.4 Flag Naming

- **Format:** kebab-case (`--max-concurrent`, `--dry-run`, `--on-error`)
- **Boolean sense:** Positive by default. Prefer `--skip-validation` over
  `--no-validate`. The exception is `--no-color` which follows the `NO_COLOR`
  convention.
- **Short flags:** Reserve for the most common flags only (`-o`, `-p`, `-v`,
  `-e`, `-d`, `-t`). Don't assign short flags to provider-specific options.

### 9.5 URL Path Patterns

Follow Kubernetes API conventions:

```
/apis/{group}/{version}/namespaces/{namespace}/{plural}/{name}
```

Provider commands using non-K8s APIs should document their URL patterns in
code comments.

See [environment-variables.md](environment-variables.md) for the canonical env var naming reference.
See [patterns.md § Provider ConfigLoader](../architecture/patterns.md#provider-configloader) for how config key names map to env vars.

### 9.6 Branding Consistency

For k6-related tools specifically, make sure to use "k6" (lowercase "k") and not "K6". This is only relevant for docstrings, documentation and other user-facing strings.
Examples: "k6 Open Source", "k6 Cloud".

### 9.7 Command Operations

> Source-of-truth hierarchy: **CONSTITUTION.md** states the invariant; the
> [Command Operation Semantics ADR](../adrs/command-operation-contract/001-command-operation-semantics.md)
> records the decision, review rubric, rationale, and rejected
> alternatives; this section is the prescriptive contributor summary. The
> allowed-operation registry and its lexical CI check land in a follow-up
> implementation PR and become the executable spelling check then; until
> that lands, this is enforced in ordinary PR review.
> **Status**: this section lands together with the ADR's acceptance
> (the proposal branch merges only in the accepted state); while the ADR
> is `proposed`, treat these rules as proposed.

A command's operation is determined by its **user-visible subject,
addressability, result cardinality, and side effects** — never by HTTP
method, API path or version, adapter registration, or transport.

**The rubric.** Command names are reviewed against the ADR §2 rubric:
what does the command actually do (operation), what does it act on
(subject), how is it spelled (direct / compound / approved shorthand),
what identity does it take and does the tree position match it
(addressability, ADR §8), what does it return, and can it change or
destroy anything. These are review lenses, not per-command metadata:
naming arguments are made from observed behavior ("this returns a
collection, so it is a `list`"), not taste. Full machine-readable
per-command contracts are a possible future design (separate ADR — see
the ADR's "Deferred, not rejected" section), not a current requirement.

**Operation vocabulary (closed core, governed extensions):**

| Family | Operations | Notes |
|--------|-----------|-------|
| Entity | `list`, `get`, `create`, `update`, `delete`, `upsert`, `patch` (reserved) | `get` = one addressable subject or singleton; `update` = typed resource update (may be partial); `upsert` = direct single-entity create-or-update in one invocation (the caller never chooses create vs update); `patch` reserved for explicit patch-document/patch-operation surfaces |
| Manifest | `push`, `pull` | GitOps tier: `push` applies selected local manifests, creating or updating each supplied resource (possibly many; it does not delete remote resources absent from the manifests) and reports a pipeline/summary result — distinguished from `upsert` by workflow, never by transport |
| Query | `query`, `search` + approved shorthands `labels`, `series`, `metrics`, `metadata` | shorthands map to real list/query semantics; the shorthand set is closed. `profile-types` is **not** a shorthand — it is renamed to `list-types` at both of its mount points (decided 2026-07-17, ADR §4). Factual caveat: several datasource reads persist discovered datasource UIDs today — docs, help text, and classification must say so (ADR §4; any refactor is separate out-of-scope work) |
| View | `status`, `timeline`, `inspect`, `diff`, `stats`, `report`, `describe` (narrow, per command) | only where output materially differs from ordinary retrieval; `show` and `summary` are not canonical |
| Domain | **candidates under review** — e.g. `acknowledge`, `silence`, `close`, `resolve`, `restore`, `sync` | governed registry; **no domain operation is ratified until the separately approved registry diff lands**; additions are a reviewed registry PR |
| Utility | `login`, `version`, `commands`, `help-tree`, `request` (surfaced as `api`), kubectl-parity `config` family (`view`, `use-context`, `current-context`, `path`, `list-contexts`, `set`, `unset`, `edit`) | mappings defined in ADR §7 (the top-level `setup` group is non-runnable; `setup status` uses the view op `status`); `gcx api` can mutate or delete arbitrary API objects and is never presented as read-only |

**Addressability.** The command path indicates the type of the first
required positional identity: parent's ID → `$PARENT $OPERATION-$CHILD
$PARENT_ID` compound; child independently addressable by its own ID →
child group; no parent identity → noun group with `list` is legal.
Selector, optional, variadic, multiple, or flag-supplied identities
cannot be read off the syntax — state them in help text; reviewers
classify them with the rubric.

**The `list-<subject>` rule.** `list-<subject>` is the approved compound
spelling of `list` when the subject is a discovery/catalog facet
(`cloudwatch list-namespaces`, `athena list-tables` — datasource-scoped,
no parent positional) or a parent-scoped collection that is not
independently addressable (`alert-groups list-alerts <group-id>`). Use
`<subject> list` when the subject is an independently addressable
resource group. "No other verbs exist today" is not by itself a
justification. The registry contains `list` once — never an entry per
`list-<subject>` spelling. (The generic allowance follows maintainer
feedback; the two addressability-based conditions are the ADR's proposed
refinement of it.)

**Protocol exception.** `gcx resources get [RESOURCE_SELECTOR]...` is a
kubectl-style protocol family: selector addressing, item-or-collection
result. The universal read-one definition of `get` does not apply to it.

**Ownership.** New command names are reviewed against the rubric in
ordinary PR review; once the allowed-operation registry lands, its
lexical CI check enforces the vocabulary — validating the terminal
tokens of runnable commands and the aliases attached to those runnable
commands; group aliases are path prefixes, inventoried and dispositioned
in the classification worksheet, never operation-validated. Spelling
only — semantic fit stays a human review judgment. Permanent central
path-keyed metadata registries are rejected as rename hazards.
