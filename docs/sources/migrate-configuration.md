---
title: Migrate your gcx configuration
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 4
---

# Migrate your `gcx` configuration files to v1

In the old `gcx` configuration format, every context had its own `grafana` and `cloud` section. The `v1` format splits the file into three sections: `stacks` for Grafana connections, `cloud` for Grafana Cloud credentials that multiple contexts can reference, and `contexts` that reference the `grafana` and `cloud` sections.

`gcx` detects config files in the legacy format and migrates a single config file automatically. If your configuration is spread across several layered files, gcx converts them in memory and asks you to migrate each file yourself, to avoid a partial migration in the event that it encounters an error in a later file. There is one exception: layered files whose entries overlap in a way the new merge rules would change are not converted at all, and every command fails with instructions to consolidate them first. This guide covers all three paths, plus how to convert a file by hand.

## Automatic migration

When `gcx` loads a legacy configuration file, it:

1. Inspects every discovered config file and checks that converting each file on its own would produce the same effective configuration as before. If it wouldn't - for example, because two layers partially overlap in a way the new merge rules would change - migration stops before touching any file, and every command that loads the configuration fails with instructions to consolidate the overlapping layers.
1. When there is a single file to migrate, it creates a backup in the same location with a `.legacy.bak` suffix. If a backup already exists, and the contents don't match the current intended backup, the migration stops. `gcx` will not overwrite or delete a backup - you can remove it yourself once you're happy with the migrated configuration.
1. Converts each context into a stack entry with the same name, moves cloud credentials into shared `cloud` entries, and rewrites the contexts as references to both.
1. Checks that the converted configuration means the same as the original before replacing the file. If that check fails, the file is left untouched and `gcx` reports an error.

{{< admonition type="caution" >}}
If your legacy file contained plaintext credentials, they remain in `<file>.legacy.bak` even after migration moves them into the OS credential store (Keychain on macOS, Credential Manager on Windows, Secret Service on Linux). `gcx` does not delete the backup. Remove it yourself once you're sure you won't roll back.
{{< /admonition >}}

Migration also never deletes credential store entries created by an older gcx.
Credentials are copied into new entries and the old ones stay, so restoring
the backup keeps working.

### Roll back an automatic migration

To roll back, copy the backup over the configuration file:

```bash
cp ~/.config/gcx/config.yaml.legacy.bak ~/.config/gcx/config.yaml
```

If the configuration file isn't writable (for example, its running in a CI image), `gcx`
migrates it in memory on every read and leaves the file alone. Read-only
commands work with that in-memory view, but anything that writes config or
credentials will fail until the file can be migrated or replaced. `gcx` prints a
warning on each invocation.

`gcx` also prints a warning when a legacy credential can't be read from the OS
credential store at migration time. The warning ends with
`reason: a legacy credential could not be read from the credential store`.
This usually means the credential store was locked, an unlock prompt was
dismissed, or `gcx` ran in a session without credential store access (SSH, CI).
Persisting the migration then could strand references to credentials it
couldn't re-store, so `gcx` waits. Unlock your credential store (or run from a
desktop session) and run any `gcx` command to complete the migration.

A file that declares any version other than `1` isn't a legacy file. gcx
rejects it before creating a backup, reading the credential store, or making
any other change. Don't edit the version number by hand - use a gcx release that
supports the declared version.

### Migrate layered files

Legacy gcx merged same-named contexts from different files field by field.
Version 1 doesn't: a stack or Cloud entry in a higher-priority file completely
replaces a same-named entry in a lower one, so one file can't mix its server
with another file's credentials. If a user file defines a complete context and
a repository file overrides only part of it, you may need to consolidate them
by hand.

When the preflight check passes for several files, gcx uses the converted
result in memory but doesn't rewrite anything. Migrate the files one at a time,
using `--file` to pick the layer. Setting `version` to `1` is a convenient way
to trigger the write - the loader has already done the conversion:

```bash
gcx config set --file user version 1
gcx config set --file local version 1
```

Before the first file changes, gcx preflights every participating layer. If
independent conversion would change the effective configuration, the command
stops with every source untouched. After each successful step, gcx prints the
paths and exact `config set --file ... version 1` commands for all legacy layers
that remain. If an interrupted sequence contains overlapping entry names, gcx
offers those completion commands only when the already-converted files still
match their private migration backups and the original all-legacy set passes
the semantic-equivalence preflight. An arbitrary version 1 file combined with
an overlapping legacy layer receives manual `gcx config edit <layer>` guidance
instead; gcx will not suggest or allow a per-layer conversion that could replace
a complete entry with a partial one. You can always open a remaining source
without loading it by running the corresponding `gcx config edit <layer>`
command.

Only use `--file system` if you own that file and have permission to update
it. If gcx reports a conflict, either move the overlapping values into one
file or rename the repository-specific context, then retry. Nothing changes -
no file, backup, or credential store entry - when the preflight check rejects
a migration.

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

Add `version: 1` at the top of the file to mark it as migrated.

Name the `cloud` entries whatever you like - contexts refer to them by name. If several contexts used the same cloud token, point them all at one shared entry. Sharing credentials like this is the main reason for the new layout.

If your legacy `cloud.token` came from the experimental OAuth sign-in rather than an access policy, it still migrates into `token` - the legacy format can't tell the two apart. The next `gcx cloud login` stores it in the entry's `oauth-token` field.

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

### Credential references

If your legacy file contains values like `keychain:gcx:prod:cloud-token`,
they are references to secrets in the OS credential store. Let the migration
move them rather than copying the strings yourself.

gcx only resolves legacy credential references from files it can trust: your
standard user config file, or a file you select yourself with `--config` or
`GCX_CONFIG`. In both cases the file must be a regular file, owned by you, and
not writable by group or others. Symlinked home or XDG paths are fine - gcx
compares the resolved locations. A reference is also only resolved when the
context and field named inside it match where it actually appears in the file.

Config files discovered automatically in system directories or repositories
are never trusted with legacy credential lookups. If a repository config
contains credential references, select it explicitly with `--config` (only if
you trust it), or replace the references with fresh credentials before
migrating. Copying a legacy reference into a version 1 file never grants
access to the secret.

Version 1 credential references are tied to the config file's path, the exact
stack or Cloud entry and field they belong to, and the credential's
destination. Copying a version 1 config to a different path copies its
structure but not access to its credentials - run `gcx login` or
`gcx cloud login` for the copied file instead of copying reference strings by
hand.

## Verify the result

After migrating, confirm the configuration parses and every context connects:

```bash
gcx config view
gcx config check
```

`gcx config view` shows the effective configuration with secrets redacted;
`gcx config check` validates every context, including connectivity, and exits
non-zero if any check fails.
