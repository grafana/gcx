# Linter rules reference

## `alertrule`

| Category | Severity | Name | Summary |
| -------- | -------- | ---- | ------- |
| `idiomatic` | `warning` | `alert-runbook-link` | Alerts should have a runbook. |
| `idiomatic` | `error` | `alert-summary` | Alerts must have a summary. |

## `dashboard`

| Category | Severity | Name | Summary |
| -------- | -------- | ---- | ------- |
| `bug` | `error` | [`target-valid-promql`](./dashboard/target-valid-promql.md) | Checks that Prometheus targets defined in dashboard panels use valid PromQL queries. |
| `idiomatic` | `warning` | [`panel-title-description`](./dashboard/panel-title-description.md) | Panels should have a title and description. |
| `idiomatic` | `warning` | [`panel-units`](./dashboard/panel-units.md) | Panels should use valid units. |
| `idiomatic` | `warning` | [`uneditable-dashboard`](./dashboard/uneditable-dashboard.md) | Dashboards should not be editable. |


## Custom rules

Custom rules are loaded via `gcx dev lint --rules <path>`. They run inside a
restricted OPA capabilities sandbox. The following built-in
OPA functions are **not available** in custom rules:

| Disallowed builtin | Reason |
| ------------------ | ------ |
| `http.send` | Network exfiltration vector |
| `net.*` (all `net.` prefixed builtins) | Network exfiltration vector |
| `opa.runtime` | Runtime introspection / environment disclosure |

No flag is provided to re-enable these functions. Bundled rules are unaffected.
