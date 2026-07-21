---
title: Migrate your gcx configuration
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 4
---

# Migrate your `gcx` configuration

`gcx` version 1 configuration files split the previous per-context layout into three sections: `stacks` for Grafana connections, `cloud` for Grafana Cloud credentials that several contexts can share, and `contexts` that bind the two together by name.

In most cases you don't need to do anything: `gcx` detects the legacy format the first time it loads your configuration file and migrates it automatically. This guide explains what the automatic migration does, and how to map a legacy configuration to the new format by hand if you prefer to review the change yourself or the automatic migration can't complete.

## What the automatic migration does

When `gcx` loads a legacy configuration file, it:

1. Writes a backup of the legacy file next to your configuration file, with a `.legacy.bak` suffix. The backup is written once and never overwritten or deleted - remove it yourself when you're happy with the migrated configuration.
1. Converts each context into a stack entry with the same name, moves cloud credentials into shared `cloud` entries, and rewrites the contexts as references to both.
1. Verifies the converted configuration is equivalent to the original before replacing the file. If verification fails, the file is left untouched and `gcx` reports an error.

The migration never deletes anything. Credentials stored in your OS keychain are copied to new entries; the original entries are kept so the backup remains fully restorable.

To roll back, copy the backup over the configuration file:

```bash
cp ~/.config/gcx/config.yaml.legacy.bak ~/.config/gcx/config.yaml
```

If the configuration file isn't writable (for example, in a CI image), `gcx` migrates in memory on every run and leaves the file alone. Commands keep working; you'll see a warning until the file is migrated or replaced.

## Map a legacy configuration to version 1

Every legacy field has exactly one new home:

| Legacy (per context)                          | Version 1                                                     |
| --------------------------------------------- | ------------------------------------------------------------- |
| `contexts.<name>.grafana.*`                   | `stacks.<name>.grafana.*`, plus `contexts.<name>.stack: <name>` |
| `contexts.<name>.cloud.token`                 | `cloud.<entry>.token`, plus `contexts.<name>.cloud: <entry>`   |
| `contexts.<name>.cloud.api-url`               | `cloud.<entry>.api-url`                                        |
| `contexts.<name>.cloud.oauth-url`             | `cloud.<entry>.oauth-url`                                      |
| `contexts.<name>.cloud.stack`                 | `stacks.<name>.slug`                                           |
| `contexts.<name>.providers.*`                 | `stacks.<name>.providers.*`                                    |
| `contexts.<name>.resources.*`                 | `stacks.<name>.resources.*`, or top-level `resources:` to apply to all stacks |
| `contexts.<name>.default-prometheus-datasource` | `contexts.<name>.datasources.prometheus`                     |
| `contexts.<name>.default-loki-datasource`     | `contexts.<name>.datasources.loki`                             |
| `contexts.<name>.default-tempo-datasource`    | `contexts.<name>.datasources.tempo`                            |
| `contexts.<name>.default-pyroscope-datasource` | `contexts.<name>.datasources.pyroscope`                       |
| `contexts.<name>.datasources.*`               | unchanged                                                      |
| `current-context`, `diagnostics`              | unchanged                                                      |

Add `version: 1` at the top level to mark the file as migrated.

Name the `cloud` entries however you like - contexts reference them by name. When several contexts carried the same cloud token, point them all at one shared entry; that deduplication is the main reason for the new layout.

### Example

A legacy configuration:

```yaml
contexts:
  prod:
    grafana:
      server: https://myorg.grafana.net
      token: "<service account token>"
    cloud:
      token: "<cloud access policy token>"
      stack: myorg
    default-prometheus-datasource: my-prom
  dev:
    grafana:
      server: https://myorg-dev.grafana.net
      token: "<service account token>"
    cloud:
      token: "<cloud access policy token>"   # same token as prod
current-context: prod
```

becomes:

```yaml
version: 1
stacks:
  prod:
    slug: myorg
    grafana:
      server: https://myorg.grafana.net
      token: "<service account token>"
  dev:
    grafana:
      server: https://myorg-dev.grafana.net
      token: "<service account token>"
cloud:
  grafana-com:
    token: "<cloud access policy token>"     # shared by both contexts
contexts:
  prod:
    stack: prod
    cloud: grafana-com
    datasources:
      prometheus: my-prom
  dev:
    stack: dev
    cloud: grafana-com
current-context: prod
```

### Keychain references

If your legacy file contains values like `keychain:gcx:prod:cloud-token`, they are references to secrets in your OS keychain. Move the strings to their new locations unchanged - `gcx` resolves them through the key embedded in the reference and rewrites them to the new naming scheme the next time it saves the configuration.

## Verify the result

After migrating, confirm the configuration parses and every context connects:

```bash
gcx config view
gcx config check
```

`gcx config view` shows the effective configuration with secrets redacted; `gcx config check` validates every context, including connectivity.
