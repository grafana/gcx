# SCIM provisioning

Use `gcx scim settings` to read or update SCIM sync settings. These commands use
Grafana's discovered native `SCIMConfig` API. The server must serve
`scim.grafana.app`. The active context supplies Grafana authentication.
The server controls access to these commands.

```bash
gcx scim settings get -o json
gcx scim settings update --user-sync=true --dry-run
gcx scim settings update --user-sync=true --group-sync=false
gcx scim settings update --reject-non-provisioned=true
gcx scim endpoint get -o json
```

An update changes only the flags that you supply. Use `=false` to disable a
setting. Other fields stay unchanged. The command reads the current resource
version before the write. If the server reports a conflict, read the settings
again and repeat the update. The command does not retry a conflict.

Enabling `--reject-non-provisioned=true` can block sign-in for every user that
SCIM did not provision. It requires confirmation. Use `--force` or
`GCX_AUTO_APPROVE=true` to bypass the prompt. A declined prompt returns exit 5
without a write. A dry run does not require confirmation. Other sync toggles
do not require confirmation.

`--dry-run` reads the current config and shows the changes without a write.
Both a dry run and a write return the same mutation result. A dry run sets
`dry_run` to `true`. Both results include the public settings in `current`
and `proposed`. The human default shows each changed value, for example
`enableGroupSync: true -> false`. Use `-o json` or `-o yaml` for the structured
result. Unknown fields stay private in both modes.

`settings get` returns a partial settings view with the resource identity and
the three public sync settings under `settings`. It is not a resource manifest.
It excludes unknown fields and annotations that can contain credentials.
Use `gcx scim settings update` to change these settings.

`gcx scim endpoint get` shows the tenant base URL for an identity provider.
It uses the active Grafana server URL and the namespace returned by the SCIM
config API. It does not guess a namespace from a stack ID. It does not include
a token. SCIM authentication remains separate from this URL.

These commands do not create SCIM tokens, change role assignments, or migrate
existing users. Use organization user commands for organization membership.

## SCIM users

Use a service account token for each SCIM user command. Supply its file with
`--token-file`. Use `--token-file -` to read the token from standard input.
The reader accepts at most 1 MiB and removes trailing line endings. It rejects
an empty token. The command does not save the token to the active context.

```bash
gcx scim users list --token-file ./scim-token
gcx scim users list --filter 'userName eq "test@example.com"' --token-file ./scim-token -o json
gcx scim users get 12345 --token-file ./scim-token -o json
gcx scim users create --user-name test@example.com --external-id test-0001 --email test@example.com --token-file ./scim-token --dry-run
gcx scim users update 12345 --display-name 'Test User' --token-file ./scim-token --dry-run
gcx scim users deactivate 12345 --token-file ./scim-token --dry-run
gcx scim users deactivate 12345 --token-file ./scim-token --force
```

The active context reads the SCIM config and resolves its tenant endpoint.
User sync must be enabled for writes. List and get do not require sync to be
enabled in gcx. Grafana controls whether these reads succeed. The command
sends the service account token only to that endpoint. It does not follow redirects or use context authentication
for SCIM user requests. The service account needs permission to manage users.

`list` fetches all matching pages. It fails if pagination stops early or repeats
users. Its table shows the user ID, user name, display name, and active state.
JSON and YAML return the server user fields, including extensions. An empty
list is an empty array. `get` takes the exact SCIM user ID.

`create` requires a user name and a stable external ID from the identity
provider. `--email` sets the work email. Grafana can link an existing account
with the same email. `update` changes only supplied attributes. It does not
change role assignments. The identity provider can later replace these values.

All three write commands accept `--dry-run`. This reads the config and shows
the request body. It sends no SCIM write. A successful write shows a short
summary. JSON, YAML, and agent output use a structured mutation result. Its `user` field contains the full server user
resource. If a PATCH returns no body, the command reads the user again.
A failed read after a successful write reports both outcomes. SCIM writes make
one request attempt. They do not retry connection errors or rate limits because
the server can apply a write before the connection fails.

`deactivate` sets `active` to `false`. Grafana keeps the user record. The command
uses the shared destructive confirmation. For standard input tokens, pass
`--force` or set `GCX_AUTO_APPROVE=true` because standard input cannot also
supply a confirmation answer. The global `--insecure-log-http-payload` option
keeps its usual behavior and can expose the token.
