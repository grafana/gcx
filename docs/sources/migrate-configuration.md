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

For a single source, `gcx` detects the legacy format the first time it loads the
file and migrates it automatically. When several config layers participate,
gcx converts them in memory only and asks you to migrate each layer explicitly;
that avoids committing one file before a later layer fails. This guide covers
both paths and the manual mapping.

## What the automatic migration does

When `gcx` loads a legacy configuration file, it:

1. Reads an exact snapshot of every discovered system, user, and repository layer and verifies that converting the layers independently will preserve the effective legacy configuration. If a partial same-named overlay would change meaning under the new atomic entry rules, migration stops before changing any file.
1. For a single-source migration, writes an exact, mode-0600 backup next to the logical configuration path with a `.legacy.bak` suffix. Backup creation is atomic and durable. An existing backup is accepted only when its bytes exactly match the legacy source being replaced. The backup is never overwritten or deleted - remove it yourself when you're happy with the migrated configuration.
1. Converts each context into a stack entry with the same name, moves cloud credentials into shared `cloud` entries, and rewrites the contexts as references to both.
1. Verifies the converted configuration is equivalent to the original before replacing the file. If verification fails, the file is left untouched and `gcx` reports an error.

{{< admonition type="caution" >}}
The backup is an exact copy, so a legacy file that contained plaintext
credentials leaves those credentials in `<file>.legacy.bak` even after the new
configuration moves them into the OS keychain. The backup stays mode `0600`,
but gcx deliberately does not delete it. Remove it yourself after you are
confident you will not roll back.
{{< /admonition >}}

The migration never deletes legacy keychain entries. Credentials are copied to
source- and destination-bound, generation-addressed entries; the old accounts
remain so an exact backup containing legacy references stays restorable.

To roll back, copy the backup over the configuration file:

```bash
cp ~/.config/gcx/config.yaml.legacy.bak ~/.config/gcx/config.yaml
```

If the configuration file isn't writable (for example, in a CI image), `gcx`
migrates it in memory on every read and leaves the file alone. Read-only
commands can use that in-memory view, but config or credential writes remain
blocked until the file can be migrated or replaced. You will see a warning on
each invocation.

A file that explicitly declares any version other than `1` is not a legacy
file. gcx rejects it before creating a backup, resolving a keychain reference,
or performing any other migration side effect. Use a gcx release that supports
the declared version instead of editing the number by hand.

### Migrate layered files

Legacy layers merged same-named contexts field-by-field. Version 1 deliberately
replaces same-named stack and Cloud entries atomically so one config source
cannot combine its destination with another source's credential. A legacy user
file that contains a complete context plus a repository file that contains a
partial overlay of that same context may therefore require manual consolidation.

When preflight succeeds for several sources, gcx uses the converted result in
memory but does not rewrite any layer. Migrate one discovered layer at a time
with the config editor's explicit layer selector; setting the already-required
version is a convenient no-op after the loader performs conversion:

```bash
gcx config set --file user version 1
gcx config set --file local version 1
```

Before the first file changes, gcx preflights every participating layer. If
independent conversion would change the effective configuration, the command
stops with every source untouched. After each successful step, gcx prints the
paths and exact `config set --file ... version 1` commands for all legacy layers
that remain. If an interrupted sequence contains overlapping entry names,
ordinary commands fail with those same deterministic completion commands rather
than a generic load error. You can always open a remaining source without
loading it by running the corresponding `gcx config edit <layer>` command.

Use `--file system` only when you own that layer and have permission to update
it. If gcx reports a semantic conflict, move the partial values into one trusted
source or give the repository-specific context a distinct name, then retry. No
source file, backup, or keychain entry is changed when preflight rejects the
migration.

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

If your legacy `cloud.token` held a token from the experimental OAuth sign-in rather than an access policy token, it migrates into `token` all the same (the two are indistinguishable in the legacy format). The next `gcx cloud login` stores it correctly in the entry's `oauth-token` field.

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

If your legacy user file contains values like
`keychain:gcx:prod:cloud-token`, let the controlled migrator move them. Legacy
references are accepted from a securely permissioned standard user source, or
from a file you deliberately select through `--config` or `GCX_CONFIG`. Explicit
consent is bound to that file's resolved canonical identity and is created only
by the high-level config loader; a library caller merely constructing an
`ExplicitConfigFile` source does not grant authority. The selected file must be
a regular file, must not be writable by group or others, and must be owned by
the current user on platforms that expose file ownership. Symlinked home/XDG
paths work because gcx compares resolved identities.

Before any keychain lookup, the sentinel's embedded owner and field must exactly
match its containing context and schema field. Auto-discovered system and
repository sources remain untrusted and perform no legacy keychain lookup;
select a file explicitly only when you trust it, or replace its references with
fresh credentials before migrating. Copying a legacy reference into a version 1
file never grants access to the secret.

Version 1 keychain references are bound to the canonical config path, exact
stack or Cloud owner kind/name, exact secret field, and normalized credential
destination. Each reference also selects an opaque random generation. Copying
a version 1 config to a different path therefore copies its structure but not
its credential authority. Run `gcx login` or `gcx cloud login` for the copied
file instead of manually copying sentinel strings.

## Verify the result

After migrating, confirm the configuration parses and every context connects:

```bash
gcx config view
gcx config check
```

`gcx config view` shows the effective configuration with secrets redacted; `gcx config check` validates every context, including connectivity.
