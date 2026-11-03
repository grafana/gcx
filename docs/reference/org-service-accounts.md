# Organization service accounts and RBAC roles

These commands use the selected Grafana context. They do not use a Grafana Cloud
access policy token. The caller must have the permissions for each API operation.
RBAC role operations require Grafana Enterprise or Grafana Cloud.

Create a service account without a basic organization role:

```sh
gcx org service-accounts create automation --role None
gcx org service-accounts list --limit 50
```

`None` is the default role. The other accepted values are `Viewer`, `Editor`, and
`Admin`. Role values are case-sensitive. The list command fetches all pages before
it applies the output limit. Use `--limit 0` to return the full list.

Find a fixed role, then use its UID to add it to the service account:

```sh
gcx org roles list --filter fixed:org.users:writer
gcx org roles assign <role-uid> --service-account-id 123
```

The filter matches a case-sensitive substring of the role name. Filtering occurs
locally after the full role list is fetched. Assignment accepts one exact UID.
The resolved role must have a name that starts with `fixed:`. The command checks
that the target is a service account. It adds an organization-scoped assignment
with `global: false`. Other assignments and the basic organization role remain.
The command does not replace all role assignments.

Create a token in a new private file:

```sh
gcx org service-accounts create-token 123 automation --ttl 24h --output-file ./token.txt
```

The default token lifetime is `1h`. Use `--ttl 24h` to change it.
The duration must be non-negative and use whole seconds. Zero means no expiry. The command creates the file with mode `0600`. It rejects
an existing file or symbolic link. The file contains the token key and a newline.
Stdout contains an artifact receipt. It does not contain the key. The global `--insecure-log-http-payload` option can expose the token in its
HTTP response dump.

The command reserves the file before it creates the token. If the file write
fails after creation, it attempts to revoke the token. If revocation also fails,
the error gives the token ID and the service account ID for manual recovery.
Treat any error after the token request as a reason to inspect the account before
you retry.

Revoke a token or delete an account:

```sh
gcx org service-accounts delete-token 123 456 --force
gcx org service-accounts delete 123 --force
```

These destructive commands use the standard confirmation prompt. Deleting the
account deletes all of its tokens. All mutations support `--dry-run`. A token or
account dry run validates inputs locally. It does not validate permissions or
reserve a file. A role assignment dry run reads the role and the service account,
but sends no write request.
