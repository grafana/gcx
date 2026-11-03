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

`--dry-run` reads the current config and shows the proposed public settings.
It does not send a write request. Config output includes only the three public
sync settings and the resource identity. It excludes unknown fields and
annotations that can contain credentials.

`gcx scim endpoint get` shows the tenant base URL for an identity provider.
It uses the active Grafana server URL and the namespace returned by the SCIM
config API. It does not guess a namespace from a stack ID. It does not include
a token. SCIM authentication remains separate from this URL.

These commands do not create SCIM tokens, change role assignments, or migrate
existing users. Use organization user commands for organization membership.
