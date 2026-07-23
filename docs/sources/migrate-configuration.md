---
title: Migrate your gcx configuration labels: products: - cloud - enterprise - oss weight: 4
---

# Migrate your `gcx` configuration files to v1 format

`gcx` is adjusting its configuration file format to make it easier to reuse credentials across contexts. The `v1` format splits the file into three sections: `stacks` for Grafana connections, `cloud` for Grafana Cloud credentials that multiple contexts can reference, and `contexts` that reference both by name.

`gcx` attempts to migrate a single legacy configuration file automatically the first time it loads it. If `gcx` printed a warning or error that linked here, your migration paused or stopped for one of a small set of reasons. Find the message you saw in [Why migration pauses or stops](#why-migration-pauses-or-stops) and follow the steps. The [field mapping](#map-a-legacy-configuration-to-version-1) at the end covers how to convert a config file manually.

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

## Why migration pauses or stops

Match the message you saw:

**"layered configuration migration is incomplete"** - your configuration is spread across several files (system, user, or a repository `.gcx.yaml`). `gcx` converts them in memory so commands keep working, but never rewrites several files behind your back: an error partway through would leave them in mixed formats without you knowing. Follow [Migrate layered files](#migrate-layered-files).

**"cannot safely auto-migrate layered legacy configuration"** or **"the overlapping entries require manual consolidation"** - two of your files define overlapping pieces of the same entry, which the v1 merge rules would combine differently than the legacy rules did. `gcx` refuses to convert rather than silently change what your configuration means. Follow [Consolidate overlapping layers](#consolidate-overlapping-layers).

**"running with in-memory config migration ... reason: a legacy credential could not be read from the credential store"** - the credential store was locked, an unlock prompt was dismissed, or `gcx` ran in a session without credential store access (SSH, CI). Persisting the migration then could strand references to credentials it couldn't re-store, so `gcx` waits. Unlock your credential store (or run from a desktop session) and run any `gcx` command; the migration completes on its own.

**"running with in-memory config migration"** with a permission-related reason - the configuration file or its directory isn't writable (common in CI images). Read-only commands keep working from the in-memory view; anything that writes configuration or credentials fails until the file is writable or replaced with a v1 file. Either fix the permissions and run any `gcx` command, or bake a migrated file into the image (see the [field mapping](#map-a-legacy-configuration-to-version-1)).

**"existing legacy config backup does not match the current source"** - a previous migration left a `.legacy.bak` and the file has since been rewritten in the legacy format (for example by an older `gcx` version). `gcx` won't overwrite the earlier backup. Compare the two files, keep the one you trust, move the backup aside, and run any `gcx` command.

**"unsupported config version"** - the file isn't legacy, it's from a newer format this `gcx` release doesn't support. Don't edit the version number by hand; upgrade `gcx` instead.

**"config migration self-check failed"** - `gcx` converted the file, checked the result against the original, and found a difference, so it left the file untouched. This indicates a bug: [report it](https://github.com/grafana/gcx/issues) with the error text, and migrate by hand in the meantime.

## Migrate layered files

The warning lists one command per remaining legacy file. Run them one at a time - each command rewrites just that file in the v1 format and leaves a `.legacy.bak` backup next to it:

```bash
gcx config set --file user version 1
gcx config set --file local version 1
```

After each step, `gcx` re-prints the commands for whatever legacy files remain, and refuses any per-file conversion that could replace a complete entry with a partial one. When it can't offer a safe command it tells you to edit the file instead. To inspect any file without loading it, run `gcx config edit <system|user|local>`.

Only run `--file system` if you own that file and have permission to change it. When all files are migrated, the warning disappears; confirm with [Verify the result](#verify-the-result).

## Consolidate overlapping layers

Legacy `gcx` merged same-named contexts from different files field by field. Version 1 doesn't: a stack or Cloud entry in a higher-priority file completely replaces a same-named entry in a lower one, so one file can never mix its server with another file's credentials. Files that relied on partial overrides need consolidating once, by hand:

1. The error names the entries that overlap. Open each file with `gcx config edit <system|user|local>` - this never loads or migrates anything.
1. Move the overriding fields into the file that owns the complete entry, or rename the overriding entry (for example, give a repository-specific context its own stack name) so nothing overlaps.
1. Run any `gcx` command. The preflight re-checks; once nothing overlaps you are directed to [Migrate layered files](#migrate-layered-files).

## Map a legacy configuration to version 1

To convert a file by hand: copy the original somewhere safe, move each field to its new home using the table, add `version: 1` at the top, and [verify](#verify-the-result). Every legacy field has exactly one new home:

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

If your legacy file contains values like `keychain:gcx:prod:cloud-token`, they are references to secrets in the OS credential store. Let the migration move them rather than copying the strings yourself.

`gcx` only resolves legacy credential references from files it can trust: your standard user config file, or a file you select yourself with `--config` or `GCX_CONFIG`. In both cases the file must be a regular file, owned by you, and not writable by group or others. Symlinked home or XDG paths are fine - `gcx` compares the resolved locations. A reference is also only resolved when the context and field named inside it match where it actually appears in the file.

Config files discovered automatically in system directories or repositories are never trusted with legacy credential lookups. If a repository config contains credential references, select it explicitly with `--config` (only if you trust it), or replace the references with fresh credentials before migrating. Copying a legacy reference into a version 1 file never grants access to the secret.

Version 1 credential references are tied to the config file's path, the exact stack or Cloud entry and field they belong to, and the credential's destination. Copying a version 1 config to a different path copies its structure but not access to its credentials - run `gcx login` or `gcx cloud login` for the copied file instead of copying reference strings by hand.

## Verify the result

After migrating, confirm the configuration parses and every context connects:

```bash
gcx config view
gcx config check
```

`gcx config view` shows the effective configuration with secrets redacted; `gcx config check` validates every context, including connectivity, and exits non-zero if any check fails.
