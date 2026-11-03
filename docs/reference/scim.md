# SCIM provisioning

Use `gcx scim config` to read or update SCIM sync settings. These commands use
Grafana's discovered native `SCIMConfig` API. The server must serve
`scim.grafana.app`. The active context supplies Grafana authentication.
The server controls access to these commands.

```bash
gcx scim config get -o json
gcx scim config update --user-sync=true --dry-run
gcx scim config update --user-sync=true --group-sync=false
gcx scim config update --reject-non-provisioned=true
gcx scim endpoint get -o json
```

An update changes only the flags that you supply. Use `=false` to disable a
setting. Other fields stay unchanged. The command reads the current resource
version before the write. If the server reports a conflict, read the settings
again and repeat the update. The command does not retry a conflict.

`--dry-run` reads the current config and shows the changes without a write.
Both a dry run and a write return the same mutation result. A dry run sets
`dry_run` to `true`. Both results include the public settings in `current`
and `proposed`. The human default shows each changed value, for example
`enableGroupSync: true -> false`. Use `-o json` or `-o yaml` for the structured
result. Unknown fields stay private in both modes.

`config get` returns a partial settings view with the resource identity and
the three public sync settings under `settings`. It is not a resource manifest.
It excludes unknown fields and annotations that can contain credentials.
Use `gcx scim config update` to change these settings.

`gcx scim endpoint get` shows the tenant base URL for an identity provider.
It uses the active Grafana server URL and the namespace returned by the SCIM
config API. It does not guess a namespace from a stack ID. It does not include
a token. SCIM authentication remains separate from this URL.

These commands do not create SCIM tokens, change role assignments, or migrate
existing users. Use organization user commands for organization membership.
