# Linter rules

Run `gcx dev lint rules` to list the rules bundled with your installed version.

- [Panel title and description](dashboard/panel-title-description.md)
- [Panel units](dashboard/panel-units.md)
- [Valid PromQL targets](dashboard/target-valid-promql.md)
- [Uneditable dashboards](dashboard/uneditable-dashboard.md)

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
