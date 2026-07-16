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

> Authoritative source: [ADR: Command Operation Contract](../adrs/command-operation-contract/001-command-operation-semantics.md).
> This section is the working summary; the ADR defines the full model,
> rationale, and rejected alternatives.

A command's operation is determined by its **user-visible subject,
addressability, result cardinality, and side effects** — never by HTTP
method, API path or version, adapter registration, or transport.

**The contract.** Every active runnable command declares (explicitly beside
its constructor, via a shared builder, or — during migration only — by
conservative inference): Operation, Subject, Surface form, Category,
Effect, Addressing, and Result shape. See the ADR §2 for the field table.

**Operation vocabulary (closed core, governed extensions):**

| Family | Operations | Notes |
|--------|-----------|-------|
| Entity | `list`, `get`, `create`, `update`, `delete`, `upsert`, `patch` (reserved) | `get` = one addressable subject or singleton; `upsert` only for genuine create-or-update backends; `patch` reserved for partial modification |
| Manifest | `push`, `pull` | GitOps tier, unchanged |
| Query | `query`, `search` + approved shorthands `labels`, `series`, `metrics`, `metadata` | shorthands map to real list/query semantics |
| View | `status`, `timeline`, `inspect`, `diff`, `stats`, `report`, `describe` (narrow, per command) | only where output materially differs from ordinary retrieval; `show` and `summary` are not canonical |
| Domain | `acknowledge`, `silence`, `close`, `resolve`, `restore`, `sync`, … | governed registry; new domain verbs are a reviewed registry PR |
| Utility | `version`, `login`, `logout`, … | closed exception lists in the ADR §8 |

**Addressability.** The command path indicates the type of the first
required positional identity: parent's ID → `$PARENT $OPERATION-$CHILD
$PARENT_ID` compound; child's ID with multiple child operations → child
group; no parent identity → noun group with `list` is legal. Selector,
optional, variadic, multiple, or flag-supplied identities require explicit
contract metadata — syntax alone cannot prove an identity's type.

**Protocol exception.** `gcx resources get [RESOURCE_SELECTOR]...` is a
kubectl-style protocol family: selector addressing, item-or-collection
result. The universal read-one definition of `get` does not apply to it.

**Ownership.** New commands declare contracts locally (constructor or
shared builder). Path-keyed registries and `Use`-string inference are
migration bootstrap only.
