---
title: Migrate your gcx configuration
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 4
---

# Migrate your `gcx` configuration files to v1 format

`gcx` is adjusting its configuration file format to make it easier to reuse credentials across contexts. This applies for `gcx` versions `v0.6.0` and later. The `v1` format splits the file into three sections: `stacks` for Grafana connections, `cloud` for Grafana Cloud credentials that multiple contexts can reference, and `contexts` that reference both by name.

`gcx` attempts to migrate a single legacy configuration file automatically the first time it loads it. If `gcx` printed a warning or error that linked here, your migration paused or stopped for one of a small set of reasons. Find the message you saw in [Why a migration paused or stopped](#why-a-migration-paused-or-stopped) and follow the steps. The [table of field mappings](#map-a-legacy-configuration-to-version-1) at the end covers how to convert a config file manually.

## Nothing is deleted

Whatever state your migration is in, nothing has been lost:

- A paused or stopped migration changes **nothing**: no file, no backup, no credential store entry. `gcx` keeps working from an in-memory conversion where it safely can.
- A completed migration replaces the file only after checking the converted result means the same as the original, and keeps the original next to it as `<file>.legacy.bak`. `gcx` never overwrites or deletes that backup.
- Credentials in the OS credential store (Keychain on macOS, Credential Manager on Windows, Secret Service on Linux) are copied into new entries; the old entries stay, so the backup remains fully usable.

To roll back a completed migration, copy the backup over the configuration file:

```bash
cp ~/.config/gcx/config.yaml.legacy.bak ~/.config/gcx/config.yaml
```

{{< admonition type="caution" >}}
If your legacy file contained plaintext credentials, they remain in `<file>.legacy.bak` even after migration moves them into the OS credential store. Remove the backup yourself once you're sure you won't roll back.
{{< /admonition >}}

## Why a migration paused or stopped

Here are the reasons the config migration might have aborted, with an explanation of how to fix it:

**"layered configuration migration is incomplete"**: your configuration is spread across several files (system, user, or a repository `.gcx.yaml`). `gcx` converts them in memory so commands keep working, but never rewrites several files for you, to avoid a state where some config files were migrated, but others were not. To remedy this, follow [How to migrate layered files](#how-to-migrate-layered-files).

**"cannot safely auto-migrate layered legacy configuration"** or **"the overlapping entries require manual consolidation"**: two of your files define overlapping pieces of the same entry, which the v1 merge rules would combine differently than the legacy rules did. To remedy this, follow [How to consolidate overlapping layers](#how-to-consolidate-overlapping-layers).

**"running with in-memory config migration ... reason: a legacy credential could not be read from the credential store"**: the credential store was locked, an unlock prompt was dismissed, or `gcx` ran in a session without credential store access (SSH, CI). Persisting the migration then could strand references to credentials it couldn't re-store, so `gcx` waits. Unlock your credential store (or run from a desktop session) and run any `gcx` command; the migration completes on its own.

**"running with in-memory config migration"** with a permission-related reason: the configuration file or its directory isn't writable. Read-only config commands keep working from the in-memory config, but anything that writes configuration or credentials fails until the file is writable or replaced with a v1 file. To remedy this, either fix the permissions and run any `gcx` command, or for CI type environments, update the config file to use the v1 format. (see the [field mapping](#map-a-legacy-configuration-to-version-1)).

**"existing legacy config backup does not match the current source"**: a previous migration left a `.legacy.bak` and the file has since been rewritten in the legacy format (for example by an older `gcx` version). `gcx` won't overwrite the earlier backup. To fix this, compare the two files, keep the one you trust and run any `gcx` command.

**"unsupported config version"**: the file isn't in the legacy format, and `gcx` doesn't recognise it. Upgrade `gcx` to support a more modern format (this is more for futureproofing - there is only v1 at the moment).

**"config migration self-check failed"**: `gcx` converted the file, checked the result against the original, and found a difference, so it left the file untouched. This indicates a bug: [report it](https://github.com/grafana/gcx/issues) with the error text, and migrate by hand in the meantime.

## How to migrate layered files

The migration warning will list each remaining legacy file that needs migrating. Migrate the config files one at a time - each command rewrites just that file in the v1 format and leaves a `.legacy.bak` backup next to it. For example:

```bash
gcx config set --file user version 1
gcx config set --file local version 1
```

After each step, `gcx` re-prints the commands for whatever legacy files still need migrating, and refuses any per-file conversion that could replace a complete entry with a partial one. When it can't offer a safe command it tells you to edit the file instead. To inspect any file without loading it, run `gcx config edit <system|user|local>`.

Be aware that editing the `system` config file will edit the config for all users on your system.

When all files are migrated, the warning will disappear. Confirm everything is ok with [Verify the result](#verify-the-result).

## How to consolidate overlapping layers

Legacy `gcx` merged contexts with the same name, but from different files, field by field. `v1` doesn't: a stack or Cloud entry in a higher-priority file completely replaces a same-named entry in a lower one. This is so one file can never mix its server with another file's credentials. Files that relied on partial overrides need consolidating once, manually:

1. The error will name the entries that overlap. Open each file with `gcx config edit <system|user|local>`.
1. Move the overriding fields into the file that owns the complete entry, or rename the overriding entry (for example, give a repository-specific context its own stack name) so nothing overlaps.
1. Run any `gcx` command. The preflight re-checks; once nothing overlaps you are directed to [Migrate layered files](#how-to-migrate-layered-files).

## Map a legacy configuration to version 1

To convert a file by hand: copy the original somewhere safe, move each field to its new home using the table, add `version: 1` at the top, and [verify](#verify-the-result). Here is a map from the old field locations to the new ones:

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

Name the `cloud` entries whatever you like - contexts refer to them by name. You can reuse cloud configs in multiple contexts.

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

If your legacy file contains values like `keychain:gcx:prod:cloud-token`, they are references to secrets in the OS credential store. Let the migration move them rather than copying the strings yourself.

`gcx` only resolves legacy credential references from files you chose and own: your standard user config file (a symlinked home or XDG path still counts), or one you selected explicitly with `--config` or `GCX_CONFIG` - and only when the file is writable by you alone. A reference must also sit where it claims to belong: the context and field named inside it have to match its location in the file. Files discovered from repositories or system directories are never trusted.

If a repository config contains credential references, select it explicitly with `--config` (only if you trust it), or replace the references with fresh credentials before migrating. Copying a legacy reference into a version 1 file never grants access to the secret.

Version 1 credential references are tied to the config file's path, the exact stack or Cloud entry and field they belong to, and the credential's destination. Copying a version 1 config to a different path copies its structure but not access to its credentials - run `gcx login` or `gcx cloud login` for the copied file instead of copying reference strings by hand.

## Verify the result

After migrating, confirm the configuration parses and every context connects:

```bash
gcx config view
gcx config check
```

`gcx config view` shows the effective configuration with secrets redacted; `gcx config check` validates every context, including connectivity, and exits non-zero if any check fails.
