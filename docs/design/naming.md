# Resource and API Naming

> Naming conventions for resource kinds, file names, config keys, environment variables, flags, and URL path patterns.

For canonical command verbs and command-tree placement, see
[command-naming.md](command-naming.md).

---

## 9. Resource and API Naming

### 9.1 Resource Kind Names

Follow Kubernetes conventions: PascalCase singular.

```
Dashboard, Folder, AlertRule, ContactPoint
```

Plural form is used in selectors: `dashboards/my-dash`, `folders/`.

### 9.2 File Naming

Pull operations write files as `{plural}.{version}.{group}/{name}.{ext}`, grouped by
`GroupResourcesByKind`. Extension matches the source format (`.yaml`, `.json`).

Example: `dashboards.v1alpha1.dashboard.grafana.app/my-dash.yaml`

The versioned directory name makes the API group and version unambiguous, which
is important when pulling multiple versions of the same resource type.

`gcx resources pull --filename-by=name` is the default and uses `metadata.name`.
With `--filename-by=title`, the filename uses a slug of `spec.title`: lowercase
ASCII letters and digits, with other characters replaced by hyphens. Repeated
hyphens collapse, edge hyphens are removed, and the slug is limited to 100
characters. Missing, non-string, empty, or entirely stripped titles fall back
to `metadata.name`. A literal title of `Resource` remains `resource`.

Title mode plans the entire batch before writing. Paths are compared without
case sensitivity within each directory. Every colliding filename gets a
`-<name>` suffix, including collisions with fallback names. Windows device names
(`con`, `prn`, `aux`, `nul`, `com1`–`com9`, `lpt1`–`lpt9`) also get this suffix.
The suffix is sanitized as an ASCII slug and limited to 100 characters; the base
is also limited to 100 characters when adding a suffix. If paths still collide,
deterministic numeric suffixes are added, avoiding every other planned path.
Resource contents and identity are unchanged.

Pull does not remove old files. Title changes, changes to the selected collision
group, or switching naming modes can leave stale files; use a clean destination
or remove stale files before pushing.

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
