# Resource and API Naming

> Naming conventions for resource kinds, file names, config keys, environment variables, flags, and URL path patterns.
> Status markers: **[CURRENT]** = enforced, **[ADOPT]** = new code must follow, **[PLANNED]** = future.

---

## 9. Resource and API Naming

### 9.1 Resource Kind Names `[CURRENT]`

Follow Kubernetes conventions: PascalCase singular.

```
Dashboard, Folder, AlertRule, ContactPoint
```

Plural form is used in selectors: `dashboards/my-dash`, `folders/`.

### 9.2 File Naming `[CURRENT]`

Pull operations write files as `{Kind}.{Version}.{Group}/{Name}.{ext}`, grouped by
`GroupResourcesByKind`. Extension matches the source format (`.yaml`, `.json`).

Example: `Dashboard.v1alpha1.dashboard.grafana.app/my-dash.yaml`

The versioned directory name makes the API group and version unambiguous, which
is important when pulling multiple versions of the same resource type.

### 9.3 Config Key Naming `[CURRENT]`

| Location | Convention | Example |
|----------|-----------|---------|
| YAML | kebab-case | `org-id`, `stack-id`, `api-token` |
| Env vars | SCREAMING_SNAKE | `GRAFANA_ORG_ID`, `GRAFANA_STACK_ID` |
| Provider env | `GRAFANA_PROVIDER_{NAME}_{KEY}` | `GRAFANA_PROVIDER_SLO_TOKEN` |

Env var keys are normalized: underscores → dashes for provider key matching.

### 9.4 Flag Naming `[ADOPT]`

- **Format:** kebab-case (`--max-concurrent`, `--dry-run`, `--on-error`)
- **Boolean sense:** Positive by default. Prefer `--skip-validation` over
  `--no-validate`. The exception is `--no-color` which follows the `NO_COLOR`
  convention.
- **Short flags:** Reserve for the most common flags only (`-o`, `-p`, `-v`,
  `-e`, `-d`, `-t`). Don't assign short flags to provider-specific options.

### 9.5 URL Path Patterns `[CURRENT]`

Follow Kubernetes API conventions:

```
/apis/{group}/{version}/namespaces/{namespace}/{plural}/{name}
```

Provider commands using non-K8s APIs should document their URL patterns in
code comments.

See [environment-variables.md](environment-variables.md) for the canonical env var naming reference.
See [provider-checklist.md § Provider ConfigLoader](provider-checklist.md#16-provider-configloader-adopt) for how config key names map to env vars.
