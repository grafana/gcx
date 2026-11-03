# Organization users

Use `gcx org users` to read and remove user memberships in the organization
of the selected context. These commands use the context's Grafana credentials.
The server checks the permissions for each request.

```sh
gcx org users list
gcx org users list --limit 0 -o json
gcx org users get alice@example.com
gcx org users delete alice@example.com --dry-run
gcx org users delete alice@example.com --force
```

Lookup accepts an exact, case-sensitive login, email, or numeric user ID.
The command reads the full organization user list for lookup. If an argument
matches more than one user, the command fails. It does not search other
organizations or the global user directory.

The list limit trims output after the full fetch. Use `--limit 0` to show all
users. Get returns one user object. JSON and YAML include organization and
user IDs, login, email, role, and the other membership fields.

Delete resolves all arguments before it sends a write request. A missing or
ambiguous identity stops the command before any removal. Multiple arguments
that resolve to the same user cause one removal. A dry run shows the resolved
user and organization IDs and sends no write request.

Delete requires confirmation. Use `--force` to skip the prompt. The shared
`GCX_AUTO_APPROVE` setting also applies. A declined prompt returns exit code 5.
Removal affects only membership in the selected organization. It keeps the Grafana account and its memberships in
other organizations.

The delete result reports success, failure, and skip counts. It gives the ID
and reason for each failed removal. The command attempts every resolved user.
A batch with failures returns a nonzero exit code. A dry-run result includes
the planned targets. Use `-o json` for structured output. Agent mode selects
structured output by default.
